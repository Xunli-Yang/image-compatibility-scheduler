import ast
import asyncio
import functools
import json
import os
import time
import yaml
from termcolor import colored
from colorama import Fore, Style
from prettytable import PrettyTable
from kubernetes import client, config
from xtesting.core import testcase


def retry(max_retries=5, delay=1):
    """
    Exponential back-off for functions execution

    :param max_retries: maximum number of retries in case of failure
    :param delay: exponential back-off time before running the function again
    """
    def decorator(func):
        @functools.wraps(func)
        async def wrapper(*args, **kwargs):
            _delay = delay
            for attempt in range(max_retries):
                try:
                    return await func(*args, **kwargs)
                except Exception as exc:
                    if attempt < max_retries - 1:
                        print(
                            f"Exception occurred for operation: {func.__name__}, retrying... ({exc})"
                        )
                        await asyncio.sleep(_delay)
                        _delay *= 2
                    else:
                        raise
        return wrapper
    return decorator


class KubernetesResources:

    HTTP_CONFLICT = 409
    HTTP_UNPROCESSABLE_CONTENT = 422
    HTTP_NOT_FOUND = 404

    NAMESPACE_LABELS = {
        "pod-security.kubernetes.io/enforce": "privileged",
        "pod-security.kubernetes.io/enforce-version": "latest",
    }

    def __init__(self):
        self.setup_client()

    def setup_client(self):
        """Setup Kubernetes client."""
        config.load_kube_config(config_file='/etc/kubeconfig')
        self.core_v1 = client.CoreV1Api()
        self.batch_v1 = client.BatchV1Api()

    def create_namespace(self, namespace):
        """Ensure namespace exists with required labels."""
        namespaces = self.core_v1.list_namespace()
        existing_namespaces = {ns.metadata.name: ns.metadata.labels for ns in namespaces.items}

        if namespace not in existing_namespaces:
            ns = client.V1Namespace(
                metadata=client.V1ObjectMeta(
                    name=namespace,
                    labels=self.NAMESPACE_LABELS,
                )
            )
            self.core_v1.create_namespace(ns)
            print(f"Created namespace {namespace} with required labels.")
        else:
            labels = existing_namespaces[namespace]
            if (
                labels.get("pod-security.kubernetes.io/enforce") != "privileged"
                or labels.get("pod-security.kubernetes.io/enforce-version") != "latest"
            ):
                print(f"Updating labels for existing namespace {namespace}.")
                ns_patch = {"metadata": {"labels": self.NAMESPACE_LABELS}}
                self.core_v1.patch_namespace(namespace, ns_patch)
                print(f"Updated labels for namespace {namespace}.")

    @retry()
    async def create_job(self, job_name, node_name, namespace, validation_args):
        """Create a Kubernetes job from a template file and retrieve its output."""
        loop = asyncio.get_running_loop()

        try:
            job_spec = self.load_job_template(job_name, namespace, node_name, validation_args)
            await loop.run_in_executor(
                None, self.batch_v1.create_namespaced_job, namespace, job_spec
            )
            print(f"Created job: {job_name}")
            await self.wait_for_job(loop, job_name, namespace)
            return await self.fetch_pod_logs(loop, job_name, namespace)
        except FileNotFoundError:
            print("Job template file not found.")
            raise
        except client.exceptions.ApiException as exc:
            if exc.status in (self.HTTP_CONFLICT, self.HTTP_UNPROCESSABLE_CONTENT):
                await self.delete_job(loop, job_name, namespace)
                raise Exception("The failing job has been removed, retrying...") from exc
            raise
        except Exception as exc:
            print(f"Failed to create job {job_name}: {exc}")
            raise

    def load_job_template(self, job_name, namespace, node_name, validation_args):
        with open("image-validation-job.template", "r") as template_file:
            job_spec = yaml.safe_load(template_file)

            # Replace placeholders from the template with actual values
            job_spec["metadata"]["name"] = job_name
            job_spec["metadata"]["namespace"] = namespace
            job_spec["spec"]["template"]["spec"]["nodeName"] = node_name

            for container in job_spec["spec"]["template"]["spec"]["containers"]:
                if container["name"] == "image-compatibility":
                    container['args'] = [
                        "--image", validation_args.get("image", ""),
                        "--output-json"
                    ]
                    if validation_args.get("plain_http", 0) == 1:
                        container['args'].append('--plain-http')
                    break

            return job_spec

    @retry()
    async def wait_for_job(self, loop, job_name, namespace):
        """Wait for the job to complete and fetch status"""
        job_status = await loop.run_in_executor(
            None, self.batch_v1.read_namespaced_job_status, job_name, namespace
        )
        if job_status.status.succeeded:
            return job_status
        if job_status.status.failed:
            raise RuntimeError(f"Job {job_name} failed.")
        raise Exception("Job is still running")

    @retry()
    async def fetch_pod_logs(self, loop, job_name, namespace):
        # Fetch pod logs
        pods = await loop.run_in_executor(
        None, functools.partial(
            self.core_v1.list_namespaced_pod,
            namespace,
            label_selector=f"job-name={job_name}"
            )
        )
        pod_name = pods.items[0].metadata.name if pods.items else None
        if not pod_name:
            raise RuntimeError(f"No pod found for job {job_name}")

        logs = await loop.run_in_executor(
            None, self.core_v1.read_namespaced_pod_log, pod_name, namespace
        )
        return pods.items[0].spec.node_name, logs

    @retry()
    async def delete_job(self, loop, job_name, namespace):
        await loop.run_in_executor(
            None, functools.partial(
                self.batch_v1.delete_namespaced_job, job_name, namespace,
                propagation_policy="Foreground"
            )
        )
        try:
            while True:
                job_status = await loop.run_in_executor(
                    None, self.batch_v1.read_namespaced_job_status, job_name, namespace
                )
                if job_status is None:
                    return
                await asyncio.sleep(2)  # Polling interval
        except client.exceptions.ApiException as exc:
            if exc.status == self.HTTP_NOT_FOUND:
                # job has not been found which is expected behaviour after deletion
                return
            raise

    def list_worker_nodes(self):
        nodes = self.core_v1.list_node(label_selector='!node-role.kubernetes.io/control-plane')
        return [ node.metadata.name for node in nodes.items ]


# TODO: add support for registry secrets
class ImageValidation(testcase.TestCase):
    """Test case for image validation."""

    NAMESPACE_LABELS = {
        "pod-security.kubernetes.io/enforce": "privileged",
        "pod-security.kubernetes.io/enforce-version": "latest",
    }

    def __init__(self, **kwargs):
        """Initialize test case."""
        super().__init__(**kwargs)
        self.setup_args()
        self.api = KubernetesResources()

    def setup_args(self):
        image = os.environ.get("IMAGE", None)
        if image is None:
            raise Exception("image argument is required!")
        self.namespace = "image-validation"
        self.validation_args = {
            "image": image,
        }
        plain_http = os.environ.get("PLAIN_HTTP", None)
        if plain_http is not None:
            self.validation_args["plain_http"] = int(plain_http)
        self.nodes = os.environ.get("NODES", None)
        if self.nodes is not None:
            self.nodes = self.nodes.split(",")
            self.nodes = list(filter(lambda x: x != "", self.nodes))

    async def execute_jobs(self, nodes):
        tasks = []
        for counter, node in enumerate(nodes, start=1):
            job_name = f"{self.validation_args.get('image', '').replace('/', '-').replace(':', '-').replace('.', '-')}-{counter}"
            tasks.append(self.api.create_job(job_name, node, self.namespace, self.validation_args))
        return await asyncio.gather(*tasks)

    def run(self, *args, **kwargs):
        """Run test case."""
        self.start_time = time.time()

        try:
            loop = asyncio.new_event_loop()
            asyncio.set_event_loop(loop)
            self.api.create_namespace(self.namespace)
            nodes = self.nodes if len(self.nodes) > 0 else self.api.list_worker_nodes()
            job_outputs = loop.run_until_complete(self.execute_jobs(nodes))
            self.compute_results(job_outputs)
        except Exception as exc:
            print(f"Unable to validate node: {exc}")
        finally:
            self.stop_time = time.time()
            loop.close()

    def compute_results(self, job_outputs):
        expected_pass = len(job_outputs)
        actual_pass = 0

        for output in job_outputs:
            node = output[0]
            job_logs = output[1]

            # TODO: currently the whole logging is redirected to stdout
            #       that must be fixed in the nfd validate-node command
            #       to allow redirect of stderr
            start_idx = job_logs.find("[{")
            end_idx = job_logs.rfind("}]") + 2
            if start_idx == -1 or end_idx == 1:
                raise ValueError("Valid JSON object not found in logs")

            # TODO: fix the json parsing, it's quite hacky now.
            #       It's related to the above issue.
            json_string = job_logs[start_idx:end_idx]
            try:
                compatibilities = json.loads(json_string)
            except json.JSONDecodeError:
                json_data = json.dumps(ast.literal_eval(json_string))
                compatibilities = json.loads(json_data)

            failed_rules = []
            for compatibility in compatibilities:
                is_image_compatible = True
                for rule in compatibility["rules"]:
                    # The rule matches the host.
                    # There is no need to report expressions.
                    if rule["isMatch"]:
                        continue
                    failed_rules.append(rule)
                    is_image_compatible = False
                if is_image_compatible:
                    actual_pass += 1
                    continue

            if len(failed_rules) > 0:
                self.print_failed_rules(node, failed_rules)

        self.result = actual_pass * 100 / expected_pass

    def print_failed_rules(self, node, rules):
        print(Fore.RED + f"\nImage: {self.validation_args['image']} is incompatible with node: {node}\n" + Style.RESET_ALL)
        for rule in rules:
            print(f"Rule: {rule['name']}")
            table = PrettyTable(field_names=["Feature", "Expression"])
            if "matchedExpressions" in rule:
                self.add_expressions(rule["matchedExpressions"], table)
            elif "matchedAny" in rule:
                for exp in rule["matchedAny"]:
                    self.add_expressions(exp["matchedExpressions"], table)
            print(colored(table, 'red'), '\n')
        print()

    def add_expressions(self, expressions, table):
        for exp in expressions:
            if exp["isMatch"]:
                continue
            table.add_row([Fore.RED + f"{exp['feature']}.{exp['name']}", f"{exp['expression']}" + Style.RESET_ALL])
