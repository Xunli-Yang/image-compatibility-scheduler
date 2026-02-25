# NFG (Node Feature Group) 多NFG缓存机制设计文档

## 概述

本文档描述了自定义K8S调度器插件中支持多个NFG的缓存机制的设计和实现。该机制旨在解决重复创建Node Feature Group（NFG）的问题，通过缓存镜像到多个NFG的映射关系，实现相同镜像的Pod复用已有的NFG，并支持一个镜像对应多个NFG的场景。

## 问题背景

在原始的调度器插件实现中，每次调度Pod时都会为Pod中的每个容器镜像创建新的NFG。这导致以下问题：

1. **资源浪费**：相同镜像的Pod会重复创建NFG
2. **性能开销**：NFD Master需要重复处理相同的兼容性规则
3. **管理复杂**：大量临时NFG对象增加Kubernetes API Server负担
4. **多NFG场景**：一个镜像可能对应多个NFG（每个兼容性规则对应一个NFG），需要支持这种复杂场景

## 设计目标

1. **减少重复创建**：相同镜像的Pod复用已有的NFG
2. **支持多NFG**：一个镜像可以对应多个NFG
3. **提高性能**：减少NFG创建和NFD处理时间
4. **保持一致性**：确保缓存的NFG仍然有效
5. **线程安全**：支持并发调度场景
6. **自动管理**：无需人工干预的缓存生命周期管理

## 架构设计

### 1. 缓存数据结构

```go
type ImageCompatibilityPlugin struct {
    // ... 其他字段
    imageToNFGCache       map[string][]string // 缓存：镜像名称 -> NFG名称列表
    imageToNFGCacheMutex  sync.RWMutex        // 保护缓存访问的互斥锁
}
```

### 2. 缓存键设计

- **键（Key）**：容器镜像的完整名称（如 `nginx:1.21-alpine`）
- **值（Value）**：对应的NodeFeatureGroup CR名称列表（如 `["image-compat-abc123", "image-compat-def456"]`）

### 3. 缓存生命周期

```
检查缓存 → 验证所有NFG → 更新缓存 → 复用/创建新NFG
```

## 实现细节

### 1. 缓存初始化

在插件初始化时创建空的缓存映射：

```go
func New(ctx context.Context, configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
    // ... 初始化其他字段
    return &ImageCompatibilityPlugin{
        // ... 其他字段
        imageToNFGCache:    make(map[string][]string),
    }, nil
}
```

### 2. 缓存管理函数

为了更好的代码组织和可维护性，将缓存操作抽取为独立的函数：

```go
// updateCacheForImage 更新指定镜像的缓存
func (f *ImageCompatibilityPlugin) updateCacheForImage(imageName string, nfgNames []string) {
    if len(nfgNames) == 0 {
        return
    }
    
    f.imageToNFGCacheMutex.Lock()
    f.imageToNFGCache[imageName] = nfgNames
    f.imageToNFGCacheMutex.Unlock()
    log.Printf("Cached NFGs %v for image %s", nfgNames, imageName)
}

// removeFromCache 从缓存中移除指定镜像
func (f *ImageCompatibilityPlugin) removeFromCache(imageName string) {
    f.imageToNFGCacheMutex.Lock()
    delete(f.imageToNFGCache, imageName)
    f.imageToNFGCacheMutex.Unlock()
    log.Printf("Removed image %s from cache", imageName)
}

// getValidCachedNFGs 获取有效的缓存NFG
func (f *ImageCompatibilityPlugin) getValidCachedNFGs(ctx context.Context, imageName, namespace string) ([]string, bool) {
    f.imageToNFGCacheMutex.RLock()
    cachedNFGs, found := f.imageToNFGCache[imageName]
    f.imageToNFGCacheMutex.RUnlock()

    if !found || len(cachedNFGs) == 0 {
        return nil, false
    }

    // 验证所有缓存的NFG是否仍然存在
    validNFGs := []string{}
    for _, nfgName := range cachedNFGs {
        if _, err := f.nfdClient.NfdV1alpha1().NodeFeatureGroups(namespace).Get(ctx, nfgName, metav1.GetOptions{}); err == nil {
            validNFGs = append(validNFGs, nfgName)
        }
    }

    if len(validNFGs) == 0 {
        // 所有缓存的NFG都无效
        f.removeFromCache(imageName)
        return nil, false
    }

    // 如果部分NFG无效，更新缓存
    if len(validNFGs) != len(cachedNFGs) {
        f.updateCacheForImage(imageName, validNFGs)
        log.Printf("Updated cache for image %s: removed %d invalid NFGs", imageName, len(cachedNFGs)-len(validNFGs))
    }

    return validNFGs, true
}
```

### 3. NFG创建流程优化

修改后的 `createNodeFeatureGroupsForImage` 函数流程：

```go
func (f *ImageCompatibilityPlugin) createNodeFeatureGroupsForImage(ctx context.Context, pod *v1.Pod, imageName, namespace string) ([]string, error) {
    // 1. 检查缓存并获取有效的NFG
    if validNFGs, found := f.getValidCachedNFGs(ctx, imageName, namespace); found {
        log.Printf("Reusing cached NFGs %v for image %s", validNFGs, imageName)
        return validNFGs, nil
    }

    // 2. 创建新的NFG
    ref, err := registry.ParseReference(imageName)
    if err != nil {
        return nil, fmt.Errorf("failed to parse image reference %s: %w", imageName, err)
    }

    ac := artifactcli.New(
        &ref,
        artifactcli.WithArgs(artifactcli.Args{PlainHttp: f.args.PlainHttp}),
        artifactcli.WithAuthDefault(),
    )

    mgmt := NewFeatureGroupManagement(ac)
    nfgs, err := mgmt.CreateNodeFeatureGroupsFromArtifact(ctx, f.nfdClient, pod, namespace)
    if err != nil {
        return nil, fmt.Errorf("failed to create NodeFeatureGroups from artifact for image %s: %w", imageName, err)
    }

    // 3. 提取NFG名称并更新缓存
    var nfgNames []string
    for _, nfg := range nfgs {
        nfgNames = append(nfgNames, nfg.Name)
    }

    // 4. 更新缓存
    f.updateCacheForImage(imageName, nfgNames)

    return nfgNames, nil
}
```

### 4. 缓存验证机制

每次使用缓存前都会验证所有NFG是否仍然存在：

1. **批量验证**：验证缓存中的所有NFG
2. **部分失效处理**：如果部分NFG失效，只移除失效的NFG
3. **完全失效处理**：如果所有NFG都失效，从缓存中移除整个条目
4. **自动清理**：当NFG不存在时自动从缓存中移除

### 5. 并发安全设计

使用读写锁（`sync.RWMutex`）保护缓存访问：

- **读锁（RLock）**：多个goroutine可以同时读取缓存
- **写锁（Lock）**：只有一个goroutine可以修改缓存

## 调度流程集成

### PreFilter阶段

1. 获取nfd-master命名空间
2. 遍历Pod中的容器镜像
3. 为每个镜像创建/复用NFG（可能多个）
4. 将创建的NFG名称列表存储在cycle state中

### Filter阶段

1. 从cycle state获取NFG名称列表
2. 收集所有NFG状态中的兼容节点（取交集）
3. 检查当前节点是否在兼容节点集合中

## 状态管理

### CompatibilityState结构

```go
type CompatibilityState struct {
    CompatibleNodes map[string]struct{}  // 兼容节点集合
    CreatedNFGs     []string             // 创建的NFG名称列表（可能包含多个NFG）
    Namespace       string               // NFG所在的命名空间
}
```

### Clone方法

实现深度复制，确保调度周期间的状态隔离：

```go
func (s *CompatibilityState) Clone() fwk.StateData {
    if s == nil {
        return &CompatibilityState{
            CompatibleNodes: map[string]struct{}{},
            CreatedNFGs:     []string{},
        }
    }
    
    // 深度复制所有字段
    newMap := make(map[string]struct{}, len(s.CompatibleNodes))
    for k, v := range s.CompatibleNodes {
        newMap[k] = v
    }
    
    newCreatedNFGs := make([]string, len(s.CreatedNFGs))
    copy(newCreatedNFGs, s.CreatedNFGs)
    
    return &CompatibilityState{
        CompatibleNodes: newMap,
        CreatedNFGs:     newCreatedNFGs,
        Namespace:       s.Namespace,
    }
}
```

## 性能优化

### 缓存命中率

- **预期命中率**：在相同镜像重复部署的场景下，缓存命中率可达90%以上
- **多NFG影响**：一个镜像对应多个NFG时，缓存验证需要检查所有NFG，但性能影响有限

### 内存使用

- 每个缓存条目：镜像名称（~100字节） + NFG名称列表（每个NFG名称~50字节）
- 预期内存使用：1000个不同镜像，每个镜像平均2个NFG，约200KB内存

### 时间开销

- **缓存命中**：~10-30ms（API调用验证多个NFG）
- **缓存未命中**：~100-500ms（创建NFG + NFD处理）
- **部分失效**：~15ms（验证并清理失效的NFG）

## 故障恢复

### 缓存一致性

1. **NFG删除**：当NFG被删除时，下次使用缓存时会自动检测并清理
2. **部分失效**：如果部分NFG失效，只移除失效的NFG，保留有效的NFG
3. **插件重启**：缓存是内存中的，插件重启后缓存会重建
4. **NFG更新**：NFG规则更新需要创建新的NFG，旧缓存会自动失效

### 错误处理

1. **API调用失败**：降级为创建新的NFG
2. **缓存损坏**：删除损坏的缓存条目
3. **并发冲突**：使用互斥锁保证数据一致性
4. **部分成功**：如果部分NFG创建失败，仍然缓存成功的NFG

## 垃圾清理机制

### 背景问题
原始实现使用跨命名空间的 OwnerReference 来实现自动清理，但这会导致 Kubernetes 垃圾收集器异常，NFG 可能在创建后很快被意外删除。

### 新清理机制设计
为了解决跨命名空间 OwnerReference 的问题，我们实现了基于标签的定期清理机制：

#### 1. 标签关联（替代 OwnerReference）
- **移除跨命名空间 OwnerReference**：避免垃圾收集器异常
- **使用标签关联 NFG 和 Pod**：
  - `pod-name`: Pod 名称
  - `pod-namespace`: Pod 命名空间
  - `pod-uid`: Pod UID
  - `managed-by`: `ImageCompatibilityFilter`
  - `temporary`: `"true"`

#### 2. 后台定期清理协程
- **启动时机**：调度器插件初始化时启动
- **检查频率**：每5分钟检查一次
- **清理逻辑**：
  1. 列出所有带有 `managed-by=ImageCompatibilityFilter` 标签的 NFG
  2. 对于每个 NFG，通过标签获取关联的 Pod 信息
  3. 检查 Pod 是否还存在
  4. 如果 Pod 不存在，删除 NFG 并清理缓存

#### 3. 清理流程
```
调度器启动
  ↓
启动后台清理协程（每5分钟运行）
  ↓
列出所有 ImageCompatibilityFilter 管理的 NFG
  ↓
对于每个 NFG:
  - 读取标签获取关联的 Pod 信息
  - 检查 Pod 是否还存在
  - 如果 Pod 不存在 → 删除 NFG + 清理缓存
  - 如果 Pod 存在 → 保留 NFG
```

#### 4. 关键优势
1. **解决根本问题**：消除跨命名空间属主引用导致的垃圾收集异常
2. **自动清理**：Pod 被删除时，对应的 NFG 会在下次检查时被清理
3. **资源友好**：每5分钟检查一次，不会对系统造成压力
4. **缓存一致性**：清理 NFG 时同时清理缓存，避免缓存污染
5. **容错性强**：即使清理失败，下次检查会重试

### 配置参数
- **清理间隔**：5分钟（硬编码，未来可配置化）
- **标签选择器**：`managed-by=ImageCompatibilityFilter`
- **关联标签**：`pod-name`, `pod-namespace`, `pod-uid`

## 监控指标

建议添加以下监控指标：

1. **缓存命中率**：`nf_cache_hits_total` / `nf_cache_requests_total`
2. **缓存大小**：`nf_cache_size`
3. **平均NFG数量**：`nf_avg_nfgs_per_image`
4. **NFG创建时间**：`nf_nfg_creation_duration_seconds`
5. **缓存验证时间**：`nf_cache_validation_duration_seconds`
6. **部分失效次数**：`nf_cache_partial_invalidations_total`

## 配置选项

未来可扩展的配置选项：

```go
type ImageCompatibilityPluginArgs struct {
    PlainHttp          bool `json:"plainHttp,omitempty"`
    CacheEnabled       bool `json:"cacheEnabled,omitempty"`       // 是否启用缓存
    CacheTTLSeconds    int  `json:"cacheTTLSeconds,omitempty"`    // 缓存TTL
    MaxCacheSize       int  `json:"maxCacheSize,omitempty"`       // 最大缓存大小
    MaxNFGsPerImage    int  `json:"maxNFGsPerImage,omitempty"`    // 每个镜像最大NFG数量
}
```

## 测试策略

### 单元测试

1. **单NFG缓存测试**：验证单个NFG的缓存机制
2. **多NFG缓存测试**：验证多个NFG的缓存机制
3. **部分失效测试**：验证部分NFG失效时的缓存清理
4. **并发测试**：验证多goroutine下的缓存安全性

### 集成测试

1. **端到端测试**：完整调度流程测试
2. **性能测试**：缓存命中率对性能的影响
3. **压力测试**：高并发场景下的稳定性
4. **多NFG场景测试**：验证一个镜像对应多个NFG的场景

## 部署注意事项

1. **内存需求**：缓存机制增加少量内存使用，多NFG场景下略有增加
2. **网络依赖**：需要访问Kubernetes API验证缓存，多NFG场景下API调用次数增加
3. **版本兼容**：与NFD版本的兼容性
4. **NFD配置**：确保NFD Master支持处理多个NFG

## 总结

多NFG缓存机制通过支持一个镜像对应多个NFG的场景，扩展了原始缓存机制的功能。该设计具有以下优点：

1. **支持复杂场景**：能够处理一个镜像对应多个NFG的实际情况
2. **智能验证**：批量验证所有缓存的NFG，优化性能
3. **部分失效处理**：智能处理部分NFG失效的情况
4. **代码组织**：将缓存操作抽取为独立函数，提高可维护性
5. **向后兼容**：完全兼容单NFG场景

通过实现此多NFG缓存机制，调度器插件能够更好地处理复杂的镜像兼容性规则，同时保持高性能和资源效率。