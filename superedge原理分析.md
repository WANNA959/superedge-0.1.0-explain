# superedge原理分析

## lite-apiserver（以v0.7.0版本为例

**本质：代理+缓存**

- 代理：边端加了lite-apiserver组件，使得所有边端节点对于云端kube-apiserver的请求，都会指向lite-apiserver组件
- 缓存：缓存了一些kube-apiserver请求，当遇到这些请求而且与APIServer不通的时候就直接返回给client

从整体上看，lite-apiserver 启动一个 HTTPS Server **接受所有 Client 的请求（实现方法是修改kubelet配置文件，将watch apiserver改为watch localhost:51003）**（https request），并**根据 request tls 证书中的 Common Name 选择对应的 ReverseProxy**（如果 request 没有 mtls 证书，则使用 default），**将 request 转发到 kube-apiserver**。

当**云边网络正常时**，将对应的返回结果（https response）返回给client，并按需将response异步存储到缓存中；

当**云边断连时**，访问kube-apiserver超时，从缓存中获取已缓存的数据返回给client，**达到边缘自治的目的**。

- **HTTPS Server** 监听 localhost 的端口（SuperEdge 中为51003）接受 Client 的 Https 请求。
  - 实现是将kubelet.conf中cluster.server指向lite-apiserver监听地址，默认localhost:51003（默认是指向apiserver的监听地址

- **Cert Mgr && Transport Mgr** 
  - Cert Mgr 负责管理连接 kube-apiserver 的 **TLS 客户端证书**。它**周期性加载配置**的TLS证书，**如果有更新，通知Transport Mgr创建或更新对应的transport**。
    - certManager.Init()
      - 构建map：commonName-*tls.Certificate

    - certManager.Start()
      - 起一个goroutine，定期更新cert（reload loadCert、handleCertUpdate
        - 定时器正常更新周期为30min，发现需要update，则马上更新

      - 通过certChannel  channel同步Transport Mgr更新（new/changed cert）
        - cm.certChannel <- commonName

  - Transport Mgr负责管理transport（传输）。它**接收Cert Mgr的通知，创建新的transport，或者关闭证书已更新的transport的旧连接**。
    - transportManager.Init()
      - 构建map：commonName-*EdgeTransport
    - transportManager.Start()
      - 起一个goroutine监控certChannel
        - new cert则创建新的transport
        - old cert则关闭transport旧连接
      - 起一个gorouine监控网卡变化 if transport changed, inform handler to create new EdgeReverseProxy
      - 同样通过transportChannel同步inform handler to create new EdgeReverseProxy
        - tm.transportChannel <- commonName

- **Proxy** 根据 request mtls 证书中的 Common Name 选择对应的 ReverseProxy（如果 request 没有 mtls 证书，则使用 default），将 request 转发到 kube-apiserver。如果请求成功，则将结果直接给 Client 返回，并调用 Cache Mgr 缓存数据；如果请求失败，则从 Cache Mgr 中读取数据给 Client。
  - h.initProxies()
    - init proxy 为每个*EdgeTransport建立EdgeReverseProxy

  - h.start()
    - 起一个goroutine监控transportChannel的commonName，在transport修改的后修改对应的ReverseProxy（更新reverseProxyMap
      - **cert change→certChannel→transport change→transportChannel→ReverseProxy change**

- **Cache Mgr** 根据 Client 的类型分别**缓存 Get 和 List 的结果数据**，并**根据 Watch 的返回值，更新对应的 List 数据**。
  - 支持多种cache类型，默认file_storage
    - 每个类型实现了Storage interface

  - 写cache时机：定义在httputil.ReverseProxy的modifyResponse下，在响应时候将response写缓存，cache对象only cache resource request（get请求）且status=http.StatusOK（cache内容包括statusCode、header、body）
    - key为keys := []string{userAgent, info.Namespace, info.Resource, info.Name, info.Subresource}  join
      - +list
    - **三种info.verb类型**（info, ok := apirequest.RequestInfoFrom(req.Context())
      - verb == constant.VerbWatch（list-watch机制
        - cacheWatch：eventType为watch.Added, watch.Modified, watch.Deleted，读list cache并更新，重新写入list cache

      - verb == constant.VerbList
        - cacheList-StoreList

      - verb == constant.VerbGet
        - cacheGet-StoreOne

  - 读cache时机：定义在httputil.ReverseProxy的ErrorHandler下，如timeout的时候执行


![图片](https://tva1.sinaimg.cn/large/e6c9d24ely1gzt8psboxuj20p20hamyp.jpg)

总的来说：对于边缘节点的组件，lite-apiserver提供的功能就是kube-APIServer，但是一方面lite-apiserver只对本节点有效，另一方面资源占用很少。

在网络通畅的情况下，**lite-apiserver组件对于节点组件来说是透明的**；而当网络异常情况，lite-apiserver组件会把本节点需要的数据返回给节点上组件，保证节点组件不会受网络异常情况影响。

## edge-health & edge-health-admission

### edge-health

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

而分布式健康检查最终的判断处理如下：

![图片](https://tva1.sinaimg.cn/large/e6c9d24ely1gzthxgqeolj20u008i3zh.jpg)

> 分布式健康检查功能由边端的 edge-health-daemon 以及云端的 edge-health-admission 组成，功能分别如下：

edge-health-daemon：对同区域边缘节点执行分布式健康检查，并向 apiserver 发送健康状态投票结果(给 node 打 annotation)，主体逻辑包括四部分功能：

- SyncNodeList：根据边缘节点所在的 zone 刷新 node cache（**该node需要检测哪些edge node**），同时更新 CheckInfoData相关数据
  - 定时器，默认周期10s：go wait.Until(check.GetNodeList, time.Duration(check.GetHealthCheckPeriod())*time.Second, ctx.Done()) 
  - 按照如下情况分类刷新 node cache：
    - 没有开启多地域检测：会**获取所有边缘节点列表并刷新 node cache**
      - kube-system namespace 下**不存在名为 edge-health-zone-config的configmap**
      - **存在edge-health-zone-config   configmap，但数据部分** **TaintZoneAdmission 为 false**
    - 开启多地域检测：存在edge-health-zone-config  configmap，且**TaintZoneAdmission 为 true。检查是否有"superedgehealth/topology-zone"标签(标示区域)**
      - 有，则获取**该label value 相同的节点列表并刷新 node cache**
      - 无，则只会将边缘节点本身添加到分布式健康检查节点列表中并刷新 **node cache(only itself)**
- ExecuteCheck：对每个边缘节点执行若干种类的健康检查插件(ping，kubelet等)，并将各插件检查分数汇总，根据用户设置的基准线得出节点是否健康的结果
  - 定时器，默认周期10s：go wait.Until(check.Check, time.Duration(check.GetHealthCheckPeriod())*time.Second, ctx.Done())
  - 目前支持ping和kubelet两种插件，**实现flag.value接口，set 添加到 PluginInfo plugin列表中**
    - 并发执行各检查插件，并同步阻塞：同步所有插件、同步某个插件下所有node得分
    - 每个plugin对应一个权重weight，正常为100*weight，不正常为0分
      - 各plugin weight之和为1
  - 同步完成后，**统计一个checked ip下所有plugin的totalscore**，若大于HealthCheckScoreLine，则**认定为normal=true，否则为false，将结果写到ResultData**
- Commun：将本节点对其它各节点健康检查的结果发送给其它节点
  - 数据有效性校验：以 kube-system 下的 hmac-config configmap **hmackey 字段为 key**，对 SourceIP 以及 CheckDetail进行 hmac（**sha256） 得到，用于判断传输数据的有效性(是否被篡改)**
  - 相互发送健康结果，故需要server（接收）+client（发送）
    - go commun.Server(ctx, &wg)
      - server：监听51005，路由/result接收请求，校验hmac检查有效性，接收并decode CommunicateData
    - go wait.Until(commun.Client, time.Duration(commun.GetPeriod())*time.Second, ctx.Done())
      - Client：desIp+51005/result发送CommunicateData（含构建hmac
  - 将得到的CommunicateData结果写入ResultData，resultDetail.time改为当前时间（不同node时间可能不同步
- Vote：对所有节点健康检查的结果分类
  - go wait.Until(vote.Vote, time.Duration(vote.GetVotePeriod())*time.Second, ctx.Done())
  - 三种vote结果
    - 如果超过一半(>)的节点对该节点的检查结果为正常，则认为该节点状态正常(注意时间差在 VoteTimeout 内)
      - 对该节点删除 nodeunhealth annotation
      - 如果node存在 NoExecute(node.kubernetes.io/unreachable) taint，则将其去掉，并更新 node.
    - 如果超过一半(>)的节点对该节点的检查结果为异常，则认为该节点状态异常(注意时间差在 VoteTimeout 内)
      - 对该节点添加 nodeunhealth=yes annotation
    - 除开上述情况，认为节点状态判断无效，对这些节点不做任何处理

### edge-health-admission

- 是一种外部构造的、自定义的Kubernetes Admission Controllers，**此处为mutating admission webhook**，通过MutatingWebhookConfiguration配置，作为外部构造被调用（大多数admission内嵌在apiserver中），会对api请求进行**准入校验**以及**修改请求对象**，下面为构建的MutatingWebhookConfiguration yaml文件
- webhook和apiserver通过AdmissionReview进行交互
  - kube-apiserver会发送AdmissionReview给Webhooks，并封装成JSON格式
  - Webhooks需要向kube-apiserver回应具有相同版本的AdmissionReview，并封装成JSON格式
    - uid：拷贝发送给webhooks的AdmissionReview request.uid字段
    - allowed：true表示准许；false表示不准许
    - ~~status：当不准许请求时，可以通过status给出相关原因(http code and message)~~
    - patch：base64编码，包含mutating admission webhook对请求对象的一系列JSON patch操作
    - patchType：目前只支持JSONPatch类型
- edge-health-admission实际上就是一个mutating admission webhook，**选择性地对endpoints以及node UPDATE请求进行修改**，包含如下处理逻辑
  - **都调用serve函数**
    - 解析request.Body为AdmissionReview对象，并赋值给requestedAdmissionReview
    - 对AdmissionReview对象执行admit函数，并赋值给回responseAdmissionReview
    - 设置responseAdmissionReview.Response.UID为请求的AdmissionReview.Request.UID
  - 区别在于admit准入函数
    - **nodeTaint**：不断修正被kube-controller-manager更新的**节点状态**，**去掉NoExecute(node.kubernetes.io/unreachable) taint，让节点不会被驱逐**
      - 检查AdmissionReview.Request.Resource是否为node资源的group/version/kind
      - 将AdmissionReview.Request.Object.Raw转化为node对象
      - 设置AdmissionReview.Response.Allowed为true，表示无论如何都准许该请求
      - 执行协助边端健康检查核心逻辑：在节点处于ConditionUnknown状态且分布式健康检查结果为正常的情况下，若节点存在NoExecute(node.kubernetes.io/unreachable) taint，则将其移除
    - **endpoint**：不断修正被kube-controller-manager更新的**endpoints状态**，将**分布式健康检查正常节点上的负载从endpoints.Subset.NotReadyAddresses移到endpoints.Subset.Addresses中，让服务依旧可用**
      - 检查AdmissionReview.Request.Resource是否为endpoints资源的group/version/kind
      - 将AdmissionReview.Request.Object.Raw转化为endpoints对象
      - 设置AdmissionReview.Response.Allowed为true，表示无论如何都准许该请求
      - 遍历endpoints.Subset.NotReadyAddresses，如果EndpointAddress所在节点处于ConditionUnknown状态且分布式健康检查结果为正常，则将该EndpointAddress从endpoints.Subset.NotReadyAddresses移到endpoints.Subset.Addresses
        - 从endpoints.Subset.NotReadyAddresses删除
        - 添加到endpoints.Subset.Addresses

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

- 不断根据 node edge-health annotation 调整 kube-controller-manager 设置的 **node taint**(去掉 NoExecute taint)以及**endpoints**(将失联节点上的 pods 从 endpoint subsets notReadyAddresses 移到 addresses中)，从而实现云端和边端共同决定节点状态
- 之所以创建edge-health-admission云端组件，是因为当云边断连时，kube-controller-manager会将失联的节点置为ConditionUnknown状态，并添加NoSchedule和NoExecute的taints；同时失联的节点上的pod从Service的Endpoint列表中移除。当edge-health-daemon在边端根据健康检查判断节点状态正常时，会更新node：去掉NoExecute taint。但是在node成功更新之后又会被kube-controller-manager给刷回去(再次添加NoExecute taint)，因此必须添加Kubernetes mutating admission webhook也即edge-health-admission，将kube-controller-manager对node api resource的更改做调整，最终实现分布式健康检查效果

## tunnel



## Reference

- https://github.com/superedge/superedge

- https://blog.csdn.net/yunxiao6/article/details/115478703?ops_request_misc=%257B%2522request%255Fid%2522%253A%2522164776571216782094823055%2522%252C%2522scm%2522%253A%252220140713.130102334.pc%255Fblog.%2522%257D&request_id=164776571216782094823055&biz_id=0&utm_medium=distribute.pc_search_result.none-task-blog-2~blog~first_rank_ecpm_v1~rank_v31_ecpm-1-115478703.nonecase&utm_term=lite&spm=1018.2226.3001.4450

- https://blog.csdn.net/yunxiao6/article/details/115066385?ops_request_misc=%257B%2522request%255Fid%2522%253A%2522164770292816780274187372%2522%252C%2522scm%2522%253A%252220140713.130102334.pc%255Fblog.%2522%257D&request_id=164770292816780274187372&biz_id=0&utm_medium=distribute.pc_search_result.none-task-blog-2~blog~first_rank_ecpm_v1~rank_v31_ecpm-6-115066385.nonecase&utm_term=superedge&spm=1018.2226.3001.4450

- https://blog.csdn.net/yunxiao6/article/details/115203688?ops_request_misc=%257B%2522request%255Fid%2522%253A%2522164770292816780274187372%2522%252C%2522scm%2522%253A%252220140713.130102334.pc%255Fblog.%2522%257D&request_id=164770292816780274187372&biz_id=0&utm_medium=distribute.pc_search_result.none-task-blog-2~blog~first_rank_ecpm_v1~rank_v31_ecpm-10-115203688.nonecase&utm_term=superedge&spm=1018.2226.3001.4450