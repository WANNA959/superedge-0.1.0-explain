# Tencent SuperEdge-0.1

Tencent公众号资料：https://mp.weixin.qq.com/s/aWgGJv5sDQX1dvGE-b_vYw

## 概述

SuperEdge 是开源的边缘容器解决方案，能够在单 Kubernetes 集群中管理跨地域的资源和容器应用

- 单 Kubernetes 集群中管理跨地域的资源和容器应用：重点词落在单和跨地域。

> 为什么要在单 Kubernetes 集群中管理跨地域的资源和容器应用呢？场景决定的

简单一个例子，有个连锁超市，10个营业网点，每个网点都有一个当日的特价广告推销程序。各个营业网点之间，以及营业网点和中心管控面之间，网络完全不通，完全独立，形成地域隔离（各个网点可能是一个小机房，也可能是一个小盒子，没有公网IP。有的网点几百个盒子，只有一个盒子可以访问外网，有的甚至所有的盒子都无法访问外网，部署的时候就相当的困难）。

每个网点的广告程序完全一样，推送的内容也完全一样，目标是能够在单 Kubernetes 集群中，管理10个营业网点，像管理一个网点一样轻松。

> 为什么不能直接用K8s

Kubernetes 要求节点和 kube-apiserver 所在 master 节点网络互通，即节点的组件能访问 master 节点的组件，master 节点的组件也能访问节点的组件。我们的第二问题是中心和网点间网络不通，直接部署原生的Kubernetes 显然是行不通的

## 架构图

![图片](https://mmbiz.qpic.cn/mmbiz_png/gBXSicjuwMvHWR92RiaEEgcpQAJemPVo2KbA01g3Wn39BxfeLvXrKucOQ1yH5F4sSFGD8zRFV6tBIu8V23AqB4FA/640?wx_fmt=png&wxfrom=5&wx_lazy=1&wx_co=1)

### 五大特性

#### 0.Kubernetes-native

SuperEdge在原生Kubernetes基础上进行了扩展，增加了边缘计算的某干组件，对Kubernetes完全无侵入；另外通过简单部署SuperEdge核心组件就可以使原生Kubernetes集群开启边缘计算功能；另外零侵入使得可以在边缘集群上部署任何Kubernetes原生工作负载(deployment, statefulset, daemonset, and etc)

#### 1.Network tunneling（tunnel隧道)云边协同能力

Network tunneling 就是图中的 tunnel 隧道。在边缘节点部署一个 tunnel-edge, 在中心部署一个 tunnel-cloud。由 tunnel-edge 主动向 tunnel-cloud 发起请求建立长连接（通常是单向可达：边缘节点可以主动访问云端的 Kube-apiserver，但是云端却无法直接访问边缘节点，所以需要云边反向隧道进行打通），这样即使边缘节点没有公网IP也能建立边端和云端的连接，让中心和边缘节点互通，本质就是 tunnel 隧道技术。

边缘部分数据是要回传云端进行分析和处理的，高效、安全的加密隧道是必要的条件

#### 2.Edge autonomy（L3级别边缘自治能力）

这个主要是解决弱网和重启的。即使有了 Network tunneling，边缘节点和云端的网络不稳定是改变不了的事实，动不动断连还是存在的。边缘自治功能满足两个边缘场景：

- 是中心和边缘之间断网，边缘节点的服务是不受影响的	
  - lite-apiserver 在云边网络正常的情况下直接从云端 Kube-apiserver 请求数据，但是云端请求不到的时候就会从本地缓存中取出相关组件管控缓存返回给请求端，保证边缘服务的稳定
- 是边缘节点重启，重启之后，边缘节点上的服务仍然能够恢复
  - lite-apiserver 把边缘节点请求中心的管理数据全部都缓存下来了，利用落盘数据在工作，维持了边缘服务，即使重启也能正常提供服务。
- lite-apiserver的其他能力：
  - 以 InCluster 方式访问 kube-apiserver
  - 支持所有类型资源的缓存，包括 CRD
  - 边缘节点安全：lite-apiserver 用代理组件的权限去请求 kube-apiserver，而不是超级权限
  - 支持多种缓存存储：Light Edge 可用本地文件存储，Heavy Edge 可以用 SQLite 等 KV 存储

#### 3.Distributed node health monitoring（分布式健康检查）edge-health组件

- 强调只有在确认边缘节点异常的情况下才会产生Pod驱逐
- 如何确认异常（而相较于云端和边缘端的连接，显然边端节点之间的连接更为稳定，具有一定的参考价值）：edge-health 运行在每个边缘节点上，探测某个区域内边缘节点的健康性。原理大概是这样的，在某个区域内，边缘节点之间是能互相访问的，然后运行在每个边缘节点的 edge-health 周期性的互相访问，确认彼此的健康性，也包括自己。按照大家的探测结果，统计出每个边缘节点的健康性，要是有XX%节点认为这个节点异常(XX%是healthcheckscoreline 参数配置的，默认100%) ，那么就把这个结果反馈给中心组件 edge-health admission。
- edge-health admission 部署在中心，结合边缘节点的状态和 edge-health 的投票结果，决定是否驱逐边缘服务到其他边缘节点。通过运用 edge-health，设置好 healthcheckscoreline，只要是边缘节点不是真正的宕机（避免网络原因被驱逐），服务就不会驱逐。一来提高了边缘服务的可用性，二来很好的扩展了 Kubernetes 驱逐在边缘的运用

![img](https://tva1.sinaimg.cn/large/e6c9d24ely1gzt7j0uch3j20za0lntb8.jpg)

> 具体来说，主要通过如下三个层面增强节点状态判断的准确性

- 每个节点定期探测其他节点健康状态
- 集群内所有节点定期投票决定各节点的状态
- 云端和边端节点共同决定节点状态

而分布式健康检查最终的判断处理如下：

![图片](https://tva1.sinaimg.cn/large/e6c9d24ely1gzthxgqeolj20u008i3zh.jpg)

#### 4.Built-in edge orchestration capability 内置资源编排能力、海量站点管理能力

- serviceGroup（中心节点application-grid controller和边缘节点application-grid wrapper 两个组件联合支持）提供功能
  - application-grid-conterlloer支持
    - 多个营业网点可以同时部署一套特价广告推销解决方案
    - 多个站点部署的服务有差别，即灰度能力
  - application-grid-wrapper支持
    - 防止边缘应用跨站点访问。因为各个站点基本提供一样的边缘服务，服务就可能会跨站点进行访问，跨站点访问会引起两个问题。A 站点可能会把 B 站点数据写紊乱，跨站点访问的延时不可控
    - 他能把一个站点的流量锁定在一个站点之内
    - application-grid-wrapper 以 DaemonSet 的形式部署在每个边缘节点上，通过给 kube-proxy 只返回本区域内的 endpoints 来达到访问在区域内闭环的目的
      - application-grid-wrapper 把 `kubernetes` 这个 Service 的 endpoint 改为 lite-apiserver 的地址， 返回给本节点 kube-proxy，即可支持 InCluster 方式访问

### 云端组件

云端除了边缘集群部署的原生Kubernetes master组件(cloud-kube-APIServer，cloud-kube-controller以及cloud-kube-scheduler)外，主要管控组件还包括：

- tunnel-cloud：负责维持与边缘节点tunnel-edge的网络隧道，目前支持TCP/HTTP/HTTPS协议。
- application-grid controller：服务访问控制ServiceGroup对应的Kubernetes Controller，负责管理DeploymentGrids以及ServiceGrids CRDs，并由这两种CR生成对应的Kubernetes deployment以及service，同时自研实现服务拓扑感知，使得服务闭环访问。
- edge-admission：通过边端节点分布式健康检查的状态报告决定节点是否健康，并协助cloud-kube-controller执行相关处理动作(打taint)。

### 边缘组件

边端除了原生Kubernetes worker节点需要部署的kubelet，kube-proxy外，还添加了如下边缘计算组件：

- lite-apiserver：边缘自治的核心组件，是cloud-kube-apiserver的代理服务，缓存了边缘节点组件对APIServer的某些请求，当遇到这些请求而且与cloud-kube-apiserver网络存在问题的时候会直接返回给client端。
- edge-health[8]：边端分布式健康检查服务，负责执行具体的监控和探测操作，并进行投票选举判断节点是否健康。
- tunnel-edge：负责建立与云端边缘集群tunnel-cloud的网络隧道，并接受API请求，转发给边缘节点组件(kubelet)。
- application-grid wrapper：与application-grid controller结合完成ServiceGrid内的闭环服务访问(服务拓扑感知)。

### lite-apiserver

从整体上看，lite-apiserver 启动一个 HTTPS Server 接受所有 Client 的请求（https request），并根据 request tls 证书中的 Common Name 选择对应的 ReverseProxy（如果 request 没有 mtls 证书，则使用 default），将 request 转发到 kube-apiserver。当云边网络正常时，将对应的返回结果（https response）返回给client，并按需将response异步存储到缓存中；当云边断连时，访问kube-apiserver超时，从缓存中获取已缓存的数据返回给client，达到边缘自治的目的。

- **HTTPS Server** 监听 localhost 的端口（SuperEdge 中为51003）接受 Client 的 Https 请求。
- **Cert Mgr && Transport Mgr** Cert Mgr 负责管理连接 kube-apiserver 的 TLS 客户端证书。它周期性加载配置的TLS证书，**如果有更新，通知Transport Mgr创建或更新对应的transport**。 **Transport Mgr负责管理transport（传输）。它接收Cert Mgr的通知，创建新的transport，或者关闭证书已更新的transport的旧连接**。
- **Proxy** 根据 request mtls 证书中的 Common Name 选择对应的 ReverseProxy（如果 request 没有 mtls 证书，则使用 default），将 request 转发到 kube-apiserver。如果请求成功，则将结果直接给 Client 返回，并调用 Cache Mgr 缓存数据；如果请求失败，则从 Cache Mgr 中读取数据给 Client。
- **Cache Mgr** 根据 Client 的类型分别缓存 Get 和 List 的结果数据，并根据 Watch 的返回值，更新对应的 List 数据。

![图片](https://tva1.sinaimg.cn/large/e6c9d24ely1gzt8psboxuj20p20hamyp.jpg)

SuperEdge通过在边端加了一层镜像lite-apiserver组件，使得所有边端节点对于云端kube-apiserver的请求，都会指向lite-apiserver组件：

![图片](https://mmbiz.qpic.cn/mmbiz_png/gBXSicjuwMvHWR92RiaEEgcpQAJemPVo2KOGK0CIPGkpu9FgtPwvVtpRScQz6kQnvm6R6xbNJdRPPxa9ZFGRJONA/640?wx_fmt=png&wxfrom=5&wx_lazy=1&wx_co=1)

而lite-apiserver其实就是个代理，缓存了一些kube-apiserver请求，当遇到这些请求而且与APIServer不通的时候就直接返回给client：

![图片](https://mmbiz.qpic.cn/mmbiz_png/gBXSicjuwMvHWR92RiaEEgcpQAJemPVo2K2YdSxV3l1MlzVnA8vnicq0pVw7Licd3rYBQkmw4RU7Rq5TWbCIZNe6nA/640?wx_fmt=png&wxfrom=5&wx_lazy=1&wx_co=1)

总的来说：对于边缘节点的组件，lite-apiserver提供的功能就是kube-APIServer，但是一方面lite-apiserver只对本节点有效，另一方面资源占用很少。在网络通畅的情况下，lite-apiserver组件对于节点组件来说是透明的；而当网络异常情况，lite-apiserver组件会把本节点需要的数据返回给节点上组件，保证节点组件不会受网络异常情况影响。

### 网络快照

通过lite-apiserver可以实现边缘节点断网情况下重启后Pod可以被正常拉起，但是根据原生Kubernetes原理，拉起后的Pod IP会发生改变，这在某些情况下是不能允许的，为此SuperEdge设计了网络快照机制保障边缘节点重启，Pod拉起后IP保存不变。

具体来说就是将节点上组件的网络信息定期快照，并在节点重启后进行恢复。

### 本地DNS解决方案

服务之间相互访问就会涉及一个域名解析的问题：通常来说在集群内部我们使用coredns来做域名解析，且一般部署为Deployment形式，但是在边缘计算情况下，节点之间可能是不在一个局域网，很可能是跨可用区的，这个时候coredns服务就可能访问不通。为了保障dns访问始终正常，SuperEdge设计了专门的本地dns解决方案，如下：

![图片](https://mmbiz.qpic.cn/mmbiz_png/gBXSicjuwMvHWR92RiaEEgcpQAJemPVo2KOiaXtfge4Y6ZZ90qatdfCVcywkwqCYwHE5NZlbsd1KEbsHl12EnXGSA/640?wx_fmt=png&wxfrom=5&wx_lazy=1&wx_co=1)

本地dns采用DaemonSet方式部署coredns，保证每个节点都有可用的coredns

同时修改每个节点上kubelet的启动参数`--cluster-dns`，将其指向本机私有IP(每个节点都相同)。这样就保证了即使在断网的情况下也能进行域名解析。

### 云边隧道tunnel

云边隧道主要用于：代理云端访问边缘节点组件的请求，解决云端无法直接访问边缘节点的问题（边缘节点没有暴露在公网中）

- 实现原理为：
  - 边缘节点上tunnel-edge主动连接云端tunnel-cloud service，tunnel-cloud service根据负载均衡策略将请求转到tunnel-cloud的具体Pod上（多个tunnel-cloud replica pod）
  - tunnel-edge与tunnel-cloud建立grpc连接后，tunnel-cloud会把自身的podIp和tunnel-edge所在节点的nodeName的映射写入DNS(tunnel dns)。grpc连接断开之后，tunnel-cloud会删除相关 Pod IP 和节点名的映射
- 而整个请求的代理转发流程如下：
  - APIServer或者其它云端的应用访问边缘节点上的kubelet或者其它应用时，tunnel-dns通过DNS劫持(将host中的节点名解析为tunnel-cloud的podIp)把请求转发到tunnel-cloud的Pod上
  - tunnel-cloud根据节点名把请求信息转发到节点名对应的与tunnel-edge建立的grpc连接上
  - tunnel-edge根据接收的请求信息请求边缘节点上的应用

![图片](https://tva1.sinaimg.cn/large/e6c9d24ely1gzth9lnb6xj20hk0i4wg0.jpg)

## 边缘计算应用场景

### 场景&特点

- 边缘计算场景中，往往会在同一个集群中管理多个边缘站点，每个边缘站点内有一个或多个计算节点；
- 同时希望在每个站点中都运行一组有业务逻辑联系的服务，每个站点内的服务是一套完整的功能，可以为用户提供服务；
- 由于受到网络限制，有业务联系的服务之间不希望或者不能跨站点访问。

### ~~ServiceGroup相关概念~~

暂不关注

## 安装

### edgeadm（除lite-apiserver

> 官方

https://www.bilibili.com/video/av333101998

https://www.cnblogs.com/tencent-cloud-native/p/15349230.html

> 实践

华为云 32G 8C

- k8s版本为superedge官方构建的1.18.2版本
  - 本机实验环境：需要修改修改edge-install/container/docker/conf/patch.json（对应覆盖docker的/etc/docker/daemon.json文件）："exec-opts": ["native.cgroupdriver=systemd"]，并重新压缩构建docker+linux-1.18.2版本，否则kubelet启动失败。
- 对于构建高版本的k8s，目前尚未成功（貌似太高版本目前还不支持？
  - 步骤大致为
    - 安装对应version的kubeadm、kubelet、kubectl
    - 将edge-install/bin下的kubectl和kubelet替换为安装版本的二进制（which kubelet = /usr/bin/kubelet）
    - 修改件"exec-opts": ["native.cgroupdriver=systemd"]，重新压缩
  - **todo：尝试1.20.0版本**

> 官方kube-linux-amd64-v1.18.2.tar.gz文件解压目录结构（再次解压docker-19.03-linux-amd64.tgz后）

![image-20220302232941641](https://tva1.sinaimg.cn/large/e6c9d24ely1gzvyhimmudj20u0110adz.jpg)

> 当前集群

![image-20220302162955539](https://tva1.sinaimg.cn/large/e6c9d24ely1gzvy8zx7ytj21zy0jcgt6.jpg)

> 加入边缘edge node（tencent cloud：xx）

```
 ./edgeadm join {huawei_cloud_ip}:6443 --token xxx --discovery-token-ca-cert-hash sha256:xxx --install-pkg-path ./kube-linux-amd64-v1.18.2.tar.gz --enable-edge=true
 
 #刷新token
 ./edgeadm token create
 
 #刷新sh256
 openssl x509 -pubkey -in /etc/kubernetes/pki/ca.crt | openssl rsa -pubin -outform der 2>/dev/null | openssl dgst -sha256 -hex | sed 's/^.* //'
```

### lite-apiserver部署pod

- 官方自治性实验方案（验证成功）：https://github.com/superedge/superedge/blob/main/docs/components/lite-apiserver_CN.md
- 手动部署lite-apiserver：https://superedge.io/zh/docs/installation/install-manually/
- lite-apiserver.yaml：https://github.com/superedge/superedge/blob/main/deployment/lite-apiserver.yaml
- **查看lite-apiserver日志（由于51003端口被占用，修改lite-apiserver的port为51004+kubelet.conf cluster.server端口为51004**

```
 docker logs xxx 
 
 kubectl logs lite-apiserver -n edge-system
 
 # I0302 14:40:18.349335       1 server.go:110] Listen on 127.0.0.1:51004
```

> 当前集群

- 新增lite-apiserver &  lite-demo pod & 一系列边缘节点组件

![image-20220307203928011](https://tva1.sinaimg.cn/large/e6c9d24ely1h01lnq6zr6j222m0rodq9.jpg)

### lite-apiserver通信

```
 # 忽略证书，匿名访问
 curl -k --tlsv1 https://127.0.0.1:51004/healthz
 
 # /api等资源不能匿名访问，带token
 TOKEN=$(kubectl describe secret $(kubectl get secrets | grep ^default | cut -f1 -d ' ') | grep -E '^token' | cut -f2 -d':' | tr -d " ")
 curl https://127.0.0.1:51004/api --header "Authorization: Bearer $TOKEN" --insecure
 curl https://localhost:51004/api --header "Authorization: Bearer $TOKEN" --insecure
```
