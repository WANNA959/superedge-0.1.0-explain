# superedge原理分析

## lite-apiserver（以v0.7.0版本为例

**本质：代理+缓存**

- 当**云边网络正常时**，作为代理，将请求转发到kube-apiserver，把对应的返回结果（https response）返回给client，并按需将response存储到缓存中；
- 当**云边断连时**，访问kube-apiserver超时，从缓存中获取已缓存的数据返回给client，**达到边缘自治的目的**。

从整体上看，lite-apiserver 启动一个 HTTPS Server **接受所有 Client 的请求**（https request），并**根据 request tls 证书中的 Common Name（识别持有者身份） 选择对应的 ReverseProxy**，**将 request 转发到 kube-apiserver**。

![图片](https://tva1.sinaimg.cn/large/e6c9d24ely1gzt8psboxuj20p20hamyp.jpg)

### HTTPS Server

- 监听 localhost 的端口（SuperEdge 中为51003）接受 Client 的 Https 请求。
  - 实现是将边缘端kubelet.conf中cluster.server指向lite-apiserver监听地址，默认localhost:51003（默认是指向apiserver的监听地址

### Cert Mgr && Transport Mgr

- Cert Mgr 负责管理连接 kube-apiserver 的 **TLS 客户端证书**。它**周期性加载配置**的TLS证书，**如果有更新，通知Transport Mgr创建或更新对应的transport**。
  - certManager.Init()
    - **构建map：commonName-*tls.Certificate**

  - certManager.Start()
    - 起一个goroutine，定期更新cert（reload loadCert、handleCertUpdate
      - 定时器正常更新周期为30min，发现需要update，则马上更新

    - 通过certChannel  channel同步Transport Mgr更新（new/changed cert）
      - **cm.certChannel <- commonName**

- Transport Mgr负责管理transport。它**接收Cert Mgr的通知，创建新的transport，或者关闭证书已更新的transport的旧连接**。
  - transportManager.Init()
    - **构建map：commonName-*EdgeTransport**
  - transportManager.Start()
    - 起一个goroutine监控certChannel
      - new cert则创建新的transport
      - old cert则关闭transport旧连接
    - 起一个gorouine监控网卡变化
    - 同样通过transportChannel同步inform handler to create new EdgeReverseProxy
      - **tm.transportChannel <- commonName**

### Proxy

-  根据 request mtls 证书中的 Common Name 选择对应的 ReverseProxy（如果 request 没有 mtls 证书，则使用 default），将 request 转发到 kube-apiserver。如果请求成功，则将结果直接给 Client 返回，并调用 Cache Mgr 缓存数据；如果请求失败，则从 Cache Mgr 中读取数据给 Client。
  - h.initProxies()
    - init proxy 为每个*EdgeTransport建立EdgeReverseProxy

  - h.start()
    - 起一个goroutine监控transportChannel的commonName，在transport修改的后修改对应的ReverseProxy（更新reverseProxyMap
      - **cert change→certChannel→transport change→transportChannel→ReverseProxy change**

### Cache Mgr

- 根据 Client 的类型分别**缓存 Get 和 List 的结果数据**，并**根据 Watch 的返回值，更新对应的 List 数据**。

- 支持多种cache类型，默认file_storage
  - 每个类型实现了Storage interface
- **写cache时机**：定义在httputil.ReverseProxy的modifyResponse下，在响应时候将response写缓存
- cache对象：只缓存对resource的get请求且status=http.StatusOK（cache内容包括statusCode、header、body）
  - key为keys := []string{userAgent, info.Namespace, info.Resource, info.Name, info.Subresource}  join
    - +list
  - **三种info.verb类型**（info, ok := apirequest.RequestInfoFrom(req.Context())
    - verb == constant.VerbList
      - cacheList-StoreList
    - verb == constant.VerbGet
      - cacheGet-StoreOne
    - verb == constant.VerbWatch（list-watch机制
      - cacheWatch：eventType为watch.Added, watch.Modified, watch.Deleted，读list cache并更新，重新写入list cache
- **读cache时机**：定义在httputil.ReverseProxy的ErrorHandler下，如timeout的时候执行

## edge-health & edge-health-admission

### 分布式健康监测

- **强调只有在确认边缘节点异常的情况下才会产生Pod驱逐**
  - 在非对称网络下，边缘场景无法采用**原生k8s的节点断连处理方法**
    - 失联的节点被置为 ConditionUnknown 状态，并被添加 NoSchedule 和 NoExecute 的 taints
    - 失联的节点上的pod被驱逐，并在其他节点上进行重建
    - 失联的节点上的pod从 Service 的 Endpoint 列表中移除

  - 相较于云端和边缘端的连接，显然边端节点之间的连接更为稳定，具有更高的参考价值


![img](https://tva1.sinaimg.cn/large/e6c9d24ely1gzt7j0uch3j20za0lntb8.jpg)

> 具体来说，主要通过如下三个层面增强节点状态判断的准确性

- 每个节点定期探测其他节点健康状态
- 集群内所有节点定期投票决定各节点的状态
- **云端和边端节点共同决定节点状态**

而分布式健康检查最终的判断**处理规则**如下：

![图片](https://tva1.sinaimg.cn/large/e6c9d24ely1gzthxgqeolj20u008i3zh.jpg)

### edge-health

对同区域边缘节点执行分布式健康检查，并向 apiserver 发送健康状态投票结果(给 node 打 annotation)，**主体逻辑包括四部分功能：**

#### 定期同步NodeList

根据边缘节点所在的 zone 刷新 nodeList（**该node需要检测哪些edge node**），同时更新 CheckInfoData相关数据（malloc memory）

- 定时器，默认周期10s：go wait.Until(**check.GetNodeList**, time.Duration(check.GetHealthCheckPeriod())*time.Second, ctx.Done()) 
- 按照如下情况分类刷新 node cache：
  - 没有开启**多地域检测**：会**获取所有边缘节点列表并刷新 node cache**
    - kube-system namespace 下**不存在名为 edge-health-zone-config的configmap**
    - **存在edge-health-zone-config   configmap，但数据部分** **TaintZoneAdmission 为 false**
  - 开启多地域检测：存在edge-health-zone-config  configmap，且**TaintZoneAdmission 为 true。检查是否有"superedgehealth/topology-zone"标签(标示区域)**
    - 有，则获取**该label value 相同的节点列表并刷新 node cache**
    - 无，则只会将边缘节点本身添加到分布式健康检查节点列表中并刷新 **node cache(only itself)**

#### 定期执行健康检查

对每个边缘节点执行若干种类的健康检查插件(ping，kubelet等)，并将**各插件检查分数**汇总，根据用户设置的**基准线**得出节点是否健康的结果

- 定时器，默认周期10s：go wait.Until(**check.Check**, time.Duration(check.GetHealthCheckPeriod())*time.Second, ctx.Done())
- 目前支持ping和kubelet两种插件（实现了checkplugin接口），**实现flag.value接口，set方法添加到 PluginInfo plugin列表中**
  - 并发执行各检查插件，并同步阻塞
    - 并发同步所有插件
    - 并发同步某个插件下所有node
  - 每个plugin对应一个权重weight，正常为100*weight，不正常为0分
    - 各plugin weight之和为1
- 所有检测同步完成后，**统计一个checked ip下所有plugin的totalscore**，若>=HealthCheckScoreLine，则**认定为normal=true，否则为false，将结果写到ResultData**

#### 边缘端通信传递检测结果

- 将本节点对其它各节点健康检查的结果发送给其它节点
  - 数据有效性校验：以 kube-system 下的 hmac-config configmap **hmackey 字段为 key**，对 SourceIP 以及 CheckDetail进行 hmac（**sha256） 得到，用于判断传输数据的有效性(是否被篡改)**
  - 相互发送健康结果，故需要server（接收）+client（发送）
    - go commun.Server(ctx, &wg)
      - server：监听51005，路由/result接收请求，校验hmac检查有效性，接收并decode CommunicateData
    - 定时器，默认10s，go wait.Until(commun.Client, time.Duration(commun.GetPeriod())*time.Second, ctx.Done())
      - Client：desIp+51005/result发送CommunicateData（含构建hmac
  - 将得到的CommunicateData结果写入ResultData，resultDetail.time改为当前时间（不同node时间可能不同步

#### 投票

- 对所有节点健康检查的结果分类
  - 定时器，默认10s，go wait.Until(**vote.Vote**, time.Duration(vote.GetVotePeriod())*time.Second, ctx.Done())
  - 三种vote结果
    - 如果超过一半(>)的节点对该节点的检查结果为正常，则认为该节点状态正常(注意时间差在 VoteTimeout 内)
      - **对该节点删除 nodeunhealth annotation**
      - 如果node存在 NoExecute(node.kubernetes.io/unreachable) taint，则将其去掉（**最坏只是NoSchedule**
    - 如果超过一半(>)的节点对该节点的检查结果为异常，则认为该节点状态异常(注意时间差在 VoteTimeout 内)
      - **对该节点添加 nodeunhealth=yes annotation**
    - 除开上述情况（偶数，vote or not各一半），认为节点状态判断无效，对这些节点不做任何处理

### edge-health-admission

- 是一种外部构造的、自定义的Kubernetes Admission Controllers，**此处为mutating admission webhook**，通过MutatingWebhookConfiguration配置，作为外部构造被调用（大多数admission内嵌在apiserver中），会对api请求进行**准入校验**以及**修改请求对象**，下面为构建的MutatingWebhookConfiguration yaml文件

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: edge-health-admission
webhooks:
  - admissionReviewVersions:
      - v1beta1
    clientConfig:
      caBundle: ...
      service:
        namespace: kube-system
        name: edge-health-admission
        path: /node-taint
    failurePolicy: Ignore
    matchPolicy: Exact
    name: node-taint.k8s.io
    namespaceSelector: {}
    objectSelector: {}
    reinvocationPolicy: Never
    
    rules:
      - apiGroups:
          - '*'
        apiVersions:
          - '*'
        operations:
          - UPDATE
        resources:
          - nodes
        scope: '*'
    sideEffects: None
    timeoutSeconds: 5
...
```

- webhook和apiserver通过AdmissionReview进行交互
  - apiserver会发送AdmissionReview给Webhooks，并封装成JSON格式
  - Webhooks需要向kube-apiserver回应具有相同版本的AdmissionReview，并封装成JSON格式
    - uid：=apiserver发送给webhooks的AdmissionReview request.uid字段
    - allowed：true表示准许；false表示不准许
    - ~~status：当不准许请求时，可以通过status给出相关原因(http code and message)~~
    - patch：base64编码，包含mutating admission webhook对请求对象的一系列JSON patch操作（局部修改）
    - patchType：目前只支持JSONPatch类型
- 为什么创建edge-health-admission云端组件？
  - 根据原生kubernetes节点断连规则：云边断连时，kube-controller-manager会将失联的节点置为ConditionUnknown状态，并添加NoSchedule和NoExecute的taints；同时失联的节点上的pod从Service的Endpoint列表中移除。
  - 当edge-health-daemon**在边端根据健康检查判断节点状态正常时，会更新node：去掉NoExecute taint。**
    - 但是在node成功更新之后又会被**kube-controller-manager给刷回去(再次添加NoExecute taint)**，因此必须添加Kubernetes mutating admission webhook也即edge-health-admission，**将kube-controller-manager对node api resource的更改做调整，最终实现分布式健康检查效果**

- edge-health-admission实际上就是一个mutating admission webhook，**选择性地对endpoints以及node 的UPDATE请求进行修改**，包含如下处理逻辑
  - **都调用serve函数：公共操作**
    - **解析request.Body为AdmissionReview对象**，并赋值给requestedAdmissionReview
    - 对AdmissionReview对象**执行admit函数**，并赋值给回responseAdmissionReview
    - 设置responseAdmissionReview.**Response.UID**为请求的AdmissionReview.**Request.UID**
  - 区别在于admit准入函数
    - **nodeTaint**：不断修正被controller-manager更新的**节点状态**，**去掉NoExecute(node.kubernetes.io/unreachable) taint，让节点不会被驱逐**
      - 检查AdmissionReview.Request.**Resource是否为node资源**的group/version/kind
      - 将AdmissionReview.Request.**Object.Raw转化为node对象**
      - 设置AdmissionReview.Response.**Allowed为true，表示无论如何都准许该请求**
      - 执行协助边端健康检查核心逻辑：**在节点处于ConditionUnknown状态且分布式健康检查结果为正常（没有nodeunhealth annotation）的情况下，若节点存在NoExecute(node.kubernetes.io/unreachable) taint，则将其移除**
    - **endpoint**：不断修正被controller-manager更新的**endpoints状态**，将**分布式健康检查正常节点上的负载从endpoints.Subset.NotReadyAddresses移到endpoints.Subset.Addresses中，让服务依旧可用**
      - 检查AdmissionReview.Request.**Resource是否为endpoints资源**的group/version/kind
      - 将AdmissionReview.Request.**Object.Raw转化为endpoints对象**
      - 设置AdmissionReview.Response.**Allowed为true，表示无论如何都准许该请求**
      - 遍历endpoints.Subset.NotReadyAddresses，如果EndpointAddress所在**节点处于ConditionUnknown状态且分布式健康检查结果为正常**，则将**该EndpointAddress从endpoints.Subset.NotReadyAddresses移到endpoints.Subset.Addresses**
        - **从endpoints.Subset.NotReadyAddresses删除**
        - **添加到endpoints.Subset.Addresses**

## tunnel

- 节点注册：node1和node2为边缘节点，tunnel-cloud将接收到请求的对应edge node（node1、2）和自身的pod Ip的mapping写入dns
- 请求的代理转发：当apiserver需要访问edge node（根据node name），根据上述dns规则，tunnel dns会返回实际和tunnel edge node连接的tunnel-cloud ip，从**而请求转发到tunnel-cloud的pod**，cloud个对应tunnel-edge建立grpc连接，tunnel-edge根据接收的请求信息请求边缘节点上的应用。

![图片](https://tva1.sinaimg.cn/large/e6c9d24ely1gzth9lnb6xj20hk0i4wg0.jpg)

### Tunnel内部模块数据交互

下图为 HTTPS 代理的数据流转，TCP 代理数据流转和 HTTPS 的类似，其中的关键步骤：

- HTTPS Server -> StreamServer（2）：**HTTPS Server 通过 Channel将 StreamMsg 发送给 Stream Server**，其中的 Channel 是根据 StreamMsg.Node 字段从 nodeContext 获取 node.Channel

- StreamServer -> StreamClient（3）: 每个**云边隧道都会分配一个 node 对象，将StreamMsg发送到 node（隧道对应的edge node） 中的 Channel** 即可把数据发往 StreamClient

- StreamServer -> HTTPS Server（5）: StreamServer **通过 Channel 将 StreamMsg 发送给 HTTPS Server**，其中的 Channel 是根据 StreamMsg.Node从nodeContext 获取 node，通过 StreamMsg.Topic 与 conn.uid 匹配获取 HTTPS 模块的 conn.Channel

> **nodeContext 和 connContext 都是做连接的管理，其交互关系如下**

- **nodeContext 管理 gRPC 长连接的和 connContext 管理的上层转发请求的连接(TCP 和 HTTPS)的生命周期是不相同的，因此需要分开管理**
  - edge/cloud收到tcp or https类型的请求的时候，将streamMsg写入到node.channel中
  - 根据interceptor，Node.recvMsg时，会调用对应的handler——接收到非hearbeat类型streamMsg（tcp or htpps），即可调用FrontendHandler、BackendHandler等将msg写入conn.channel中
  - edge/cloud发送tcp ort https类型的请求的时候，从conn.channel中读streamMsg，并构建tcp请求发送

![img](https://tva1.sinaimg.cn/large/e6c9d24ely1h0finexx5fj20fb0idwfm.jpg)

### Tunnel连接管理

Tunnel 管理的连接可以分为**底层连接(云端隧道的 gRPC 连接)和上层应用连接(HTTPS 连接和 TCP 连接)**，连接异常的管理的可以分为以下几种场景：

- gRPC 连接正常，上层连接异常：以 HTTPS 连接为例，tunnel-edge 的 HTTPS Client 与边缘节点 Server 连接异常断开，会发送 StreamMsg **(StreamMsg.Type=CLOSE)** 消息，tunnel-cloud 在接收到 StreamMsg 消息之后会主动关闭 HTTPS Server与HTTPS Client 的连接。
- gRPC 连接异常：gRPC 连接异常，Stream 模块会根据与 gPRC 连接绑定的 node.connContext，向 HTTPS 和 TCP 模块发送 StreamMsg(StreamMsg.Type=CLOSE)，HTTPS 或 TCP 模块接收消息之后主动断开连接。

### Stream模块

- **Stream 模块负责建立 gRPC连接以及通信(云边隧道)**
  - stream.send起两个goroutine（wrappedClientStream.SendMsg + wrappedClientStream.RecvMsg）
  - cloud端：stream.go go connect.StartServer() →grpcserver.go StartServer →streamserver.go TunnelStreaming →streaminterceptor.go wrappedClientStream（sendMsg、RecvMsg）
  - edge端：stream.go go connect.StartSendClient()→grpcclient.go StartSendClient →streamclient.go Send()→streaminterceptor.go wrappedServerStream（sendMsg、RecvMsg）
- **边缘节点上 tunnel-edge 主动连接云端 tunnel-cloud service**，tunnel-cloud service 根据负载均衡策略将请求转到tunnel-cloud pod
- tunnel-edge 与 tunnel-cloud 建立 gRPC 连接后，tunnel-cloud 会把自身的 podIp 和 tunnel-edge 所在节点的 nodeName 的映射写入**tunnel-coredns**。gRPC 连接断开之后，tunnel-cloud 会删除相关 podIp 和节点名的映射
- tunnel-edge 会利用边缘节点名以及 token 构建 gRPC 连接，**而 tunnel-cloud 会通过认证信息解析 gRPC 连接对应的边缘节点（一个cloud可能对应多个edge node）**，并对每个边缘节点分别构建一个 wrappedServerStream 进行处理(同一个 tunnel-cloud 可以处理多个 tunnel-edge 的连接)
  - ServerStreamInterceptor中ParseToken获取nodeName，构建 newServerWrappedStream(ss, auth.NodeName)
- **tunnel-cloud** 每隔一分钟(考虑到 configmap 同步 tunnel-cloud 的 pod 挂载文件的时间)**向 tunnel-coredns 的 hosts 插件的配置文件对应 configmap 同步一次边缘节点名以及 tunnel-cloud podIp 的映射（**内存同步configmap
  - go connect.SynCorefile()  1min为周期更新

![img](https://tva1.sinaimg.cn/large/e6c9d24ely1h0fj65nsixj20k108xgm1.jpg)

- tunnel-edge **每隔一分钟会向 tunnel-cloud 发送代表该节点正常的心跳 StreamMsg**，而 tunnel-cloud 在接受到该心跳后会进行回应(心跳是为了探测 gRPC Stream 流是否正常)
  - edge端wrappedClientStream.SendMsg发送heartbeat给cloud，若在1min内没有收到cloud端的回复 or 有error产生，则构建type=closed类型的StreamMsg
- **StreamMsg类型： 包括心跳，TCP 代理以及 HTTPS 请求等不同类型消息**
- tunnel-cloud **通过 context.node 区分与不同边缘节点 gRPC 连接隧道**

### ~~Https代理模块~~

- HTTPS：负责**建立云边 HTTPS 代理**(eg：云端 kube-apiserver <-> 边端 kubelet)，并传输数据
- 作用与 TCP 代理类似，不同的是 **tunnel-cloud 会读取云端组件 HTTPS 请求中携带的边缘节点名，并尝试建立与该边缘节点的 HTTPS 代理**；而**不是像 TCP 代理一样随机选择一个云边隧道**进行转发
- 云端 apiserver 或者其它云端的应用访问边缘节点上的 kubelet 或者其它应用时,tunnel-dns 通过DNS劫持(将 Request host 中的节点名解析为 tunnel-cloud 的 podIp)把请求转发到 tunnel-cloud 的pod上,tunnel-cloud 把请求信息封装成 StreamMsg 通过与节点名对应的云边隧道发送到 tunnel-edge，tunnel-edge 通过接收到的 StreamMsg 的 Addr 字段和配置文件中的证书与边缘端 Server 建立 TLS 连接，并将 StreamMsg 中的请求信息写入 TLS 连接。tunnel-edge 从 TLS 连接中读取到边缘端 Server 的返回数据，将其封装成 StreamMsg 发送到 tunnel-cloud，tunnel-cloud 将接收到数据写入云端组件与 tunnel-cloud 建立的连接中。

### TCP代理模块

- TCP：负责在**多集群管理中建立云端与边端的 TCP 代理**
  - cloud端
    - 起一个tcp server：net.Listen("tcp", front)
    - **云端组件作为client主动建立连接**
  - edge端
    - edge端组件作为server
    - FrontendHandler中，**edge作为客户端net.DialTCP("tcp", nil, tcpAddr)，主动建立连接连接**
  - edge/cloud端都起两个goroutine
    - read：读edge/cloud端组件发来的tcp请求，并构建为StreamMsg Send2Node发送到node channel中
    - write：msg := <-tcp.C.ConnRecv()，msg.data发送给edge/cloud端组件
- **云端组件通过 TCP 模块访问边缘端的 Server**
  - 云端的 TCP Server 在接收到请求会将请求封装成 StreamMsg 通过云边隧道(在已连接的隧道中随机选择一个，因此推荐在只有一个 tunnel-edge 的场景下使用 TCP 代理)发送到 tunnel-edge
  - tunnel-edge 通过接收到 StreamMag 的Addr字段与边缘端 Server 建立TCP 连接，并将请求写入 TCP 连接。
  - tunnel-edge 从 TCP 连接中读取边缘端 Server 的返回消息
  - 通过云边缘隧道发送到tunnel-cloud
  - tunnel-cloud 接收到消息之后将其写入云端组件与 TCP Server 建立的连接

## Reference

- https://github.com/superedge/superedge

- https://blog.csdn.net/yunxiao6/article/details/115478703?ops_request_misc=%257B%2522request%255Fid%2522%253A%2522164776571216782094823055%2522%252C%2522scm%2522%253A%252220140713.130102334.pc%255Fblog.%2522%257D&request_id=164776571216782094823055&biz_id=0&utm_medium=distribute.pc_search_result.none-task-blog-2~blog~first_rank_ecpm_v1~rank_v31_ecpm-1-115478703.nonecase&utm_term=lite&spm=1018.2226.3001.4450

- https://blog.csdn.net/yunxiao6/article/details/115066385?ops_request_misc=%257B%2522request%255Fid%2522%253A%2522164770292816780274187372%2522%252C%2522scm%2522%253A%252220140713.130102334.pc%255Fblog.%2522%257D&request_id=164770292816780274187372&biz_id=0&utm_medium=distribute.pc_search_result.none-task-blog-2~blog~first_rank_ecpm_v1~rank_v31_ecpm-6-115066385.nonecase&utm_term=superedge&spm=1018.2226.3001.4450

- https://blog.csdn.net/yunxiao6/article/details/115203688?ops_request_misc=%257B%2522request%255Fid%2522%253A%2522164770292816780274187372%2522%252C%2522scm%2522%253A%252220140713.130102334.pc%255Fblog.%2522%257D&request_id=164770292816780274187372&biz_id=0&utm_medium=distribute.pc_search_result.none-task-blog-2~blog~first_rank_ecpm_v1~rank_v31_ecpm-10-115203688.nonecase&utm_term=superedge&spm=1018.2226.3001.4450

- https://blog.csdn.net/yunxiao6/article/details/117023803
- https://github.com/khalid-jobs/tunnel



