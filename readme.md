# My-RPC

> 简易rpc框架，实现net/rpc并增加协议交换、服务注册与发现、负载均衡、超时处理等特性。

### 概述
- 从零实现了 net/rpc 包，具有基本调用功能 \
- 支持通过不同的编码格式进行序列化和反序列化 \
- 高性能客户端，支持并发和异步请求\
- 支持客户端和服务端的超时处理\
- 具有注册中心，通过心跳机制进行健康检查\
- 具有服务发现功能，支持多种负载均衡算法

### 遇到的问题
#### 1. Goroutine 泄漏
使用 finish chan 来通知协程关闭，避免协程泄漏
```go
finish := make(chan struct{})
defer close(finish)

go func() {
    ...
    select {
    case <-finish:
        // avoid goroutine memory leak
        ... your code ...
        return
    ...
}()
```