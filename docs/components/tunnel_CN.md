
# tunnel

tunnel是云边端通信的隧道，分为tunnel-cloud和tunnel-edge，分别承担云边隧道的两端

## 作用
- 代理云端请求访问边缘组件，解决云边端无法直接通信的问题（边缘节点无公网IP）；

## 架构图

- node1和node2为边缘节点，tunnel-cloud将接收到请求的对应edge node（node1、2）和自身的pod Ip的mapping写入dns
- 当apiserver需要访问edge node（根据node name），根据上述dns规则，tunnel dns会返回实际和tunnel edge node连接的tunnel-cloud ip，从**而请求转发到tunnel-cloud的pod**，cloud个对应tunnel-edge建立grpc连接

<div align="left">
  <img src="../img/tunnel.png" width=70% title="tunnel Architecture">
</div>

## 实现方案
### 节点注册
   - 边缘节点上tunnel-edge主动连接云端tunnel-cloud service,service根据负载均衡策略将请求转到tunnel-cloud的pod。
   - tunnel-edge与tunnel-cloud建立grpc连接后，tunnel-cloud会把自身的podIp和tunnel-edge所在节点的nodeName的映射写入DNS。grpc连接断开之后，tunnel-cloud会删除podIp和节点名映射。

### 请求的代理转发
   - apiserver或者其它云端的应用访问边缘节点上的kubelet或者其它应用时,tunnel-dns通过DNS劫持(将host中的节点名解析为tunnel-cloud的podIp)把请求转发到tunnel-cloud的pod。
   - tunnel-cloud根据节点名把请求信息转发到节点名对应的与tunnel-edge建立的grpc连接。
   - tunnel-edge根据接收的请求信息请求边缘节点上的应用。