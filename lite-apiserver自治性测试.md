## superedge的lite-apiserver是否可以脱离kube-apiserver对用户提供CRD

**不可以**

当lite-apiserver和kube-apiserver保持连接的时候，可以正常对pod、service、deployment进行创建、查找、删除

但是当kube-apiserver断开连接，lite-apiserver无法进行创建、查找、删除操作，但是运行在edge node上的pod可以正常运行（参考官方自治性实验：https://github.com/superedge/superedge/blob/main/docs/components/lite-apiserver_CN.md）

具体实验测试见后文

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

> 加入边缘edge node（tencent cloud：101.43.253.110）

```
./edgeadm join 124.70.70.137:6443 --token cs35ep.u6v5qhdryr61jkg4 --discovery-token-ca-cert-hash sha256:06d29b2856daff1e5569e4ede55cfd816c8a447220cfed15f327072f4b3af227 --install-pkg-path ./kube-linux-amd64-v1.18.2.tar.gz --enable-edge=true

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

```shell
docker logs xxx 

kubectl logs lite-apiserver -n edge-system

# I0302 14:40:18.349335       1 server.go:110] Listen on 127.0.0.1:51004
```

> 当前集群

- 新增lite-apiserver &  lite-demo pod & 一系列边缘节点组件

![image-20220307203928011](https://tva1.sinaimg.cn/large/e6c9d24ely1h01lnq6zr6j222m0rodq9.jpg)

### lite-apiserver通信

```shell
# 忽略证书，匿名访问
curl -k --tlsv1 https://127.0.0.1:51004/healthz

# /api等资源不能匿名访问，带token
TOKEN=$(kubectl describe secret $(kubectl get secrets | grep ^default | cut -f1 -d ' ') | grep -E '^token' | cut -f2 -d':' | tr -d " ")
curl https://127.0.0.1:51004/api --header "Authorization: Bearer $TOKEN" --insecure
curl https://localhost:51004/api --header "Authorization: Bearer $TOKEN" --insecure
```

## k8s Rest API

```
curl https://192.168.0.48:6443/api --header "Authorization: Bearer $TOKEN" --insecure
```

### 用RestAPI创建pod

**User “system:serviceaccount:default:default“ cannot get resource “endpoints“ in API group ““问题解决**

- ~~创建k8s集群最高权限的serviceaccount，可以对任何资源对象操作：https://blog.csdn.net/u013189824/article/details/110232938~~
  - ~~客户端 rest api：默认缺少很多权限~~
    - ~~此外有kubectl、集群内部方法~~
  - ~~创建namespace=rest-op下的最高权限（后面定义pod也需要在rest-op下~~

```yaml
---
apiVersion: rbac.authorization.k8s.io/v1beta1
kind: ClusterRoleBinding
metadata:
  name: rest-op #ClusterRoleBinding的名字
subjects:
  - kind: ServiceAccount
    name: rest-op #serviceaccount资源对象的name
    namespace: rest-op #serviceaccount的namespace
roleRef:
  kind: ClusterRole 
  name: cluster-admin #k8s集群中最高权限的角色
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: rest-op # ServiceAccount的名字
  namespace: rest-op # serviceaccount的namespace
  labels:
    app: rest-op #ServiceAccount的标签
```

- **生成客户端认证：https://help.aliyun.com/document_detail/160530.html**

```(
cat  ~/.kube/config |grep client-certificate-data | awk -F ' ' '{print $2}' |base64 -d > ./client-cert.pem
cat  ~/.kube/config |grep client-key-data | awk -F ' ' '{print $2}' |base64 -d > ./client-key.pem
APISERVER=`cat  ~/.kube/config |grep server | awk -F ' ' '{print $2}'`
```

- nginx.yaml文件内容

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: rest-nginx
  namespace: rest-op
  labels:
    name: rest-nginx
spec:
  containers:
  - name: rest-nginx
    image: nginx:latest
    imagePullPolicy: IfNotPresent
    ports:
    - containerPort: 80
  restartPolicy: Always

```

### 部署nginx pod服务（kube-apiserver

```shell
curl --cert client-cert.pem --key client-key.pem  -k https://192.168.0.48:6443/api/v1/namespaces/rest-op/pods POST -H 'Content-Type: application/yaml' --data-binary @nginx.yaml  --insecure

#cert or token成功
curl --cert client-cert.pem --key client-key.pem  -k https://192.168.0.48:6443/api/v1/namespaces/rest-op/pods 
curl -k --header "Authorization: Bearer $TOKEN" https://192.168.0.48:6443/api/v1/namespaces/rest-op/pods --insecure

#查询namespace=rest-op下的pod（成功
curl -k --cert client-cert.pem --key client-key.pem https://192.168.0.48:6443/api/v1/namespaces/rest-op/pods 
curl -k https://192.168.0.48:6443/api/v1/namespaces/rest-op/pods 
curl -k --header "Authorization: Bearer $TOKEN" https://192.168.0.48:6443/api/v1/namespaces/rest-op/pods 

#创建pod（成功
curl -k -X POST --cert client-cert.pem --key client-key.pem https://192.168.0.48:6443/api/v1/namespaces/rest-op/pods -H 'Content-Type: application/yaml' --data-binary @nginx.yaml  --insecure
curl -k -X POST --header "Authorization: Bearer $TOKEN" https://192.168.0.48:6443/api/v1/namespaces/rest-op/pods -H 'Content-Type: application/yaml' --data-binary @nginx.yaml  --insecure

#curl nginx服务（成功
curl $(kubectl get pods --all-namespaces -o wide | grep nginx | awk '{print $7}'):80

#删除pod（成功
curl -k -X DELETE  --cert client-cert.pem --key client-key.pem  https://192.168.0.48:6443/api/v1/namespaces/rest-op/pods/
curl -k -X DELETE  --header "Authorization: Bearer $TOKEN" https://192.168.0.48:6443/api/v1/namespaces/rest-op/pods/
```

### 部署nginx pod服务（lite-apiserver，kube-apiserver保持连接

- Edge node无法通过自身的.kube/config生成client.key（失败），只能通过master分发

```shell
# bearer方式 or cert方式均可

#匿名用户权限问题（否则下面操作全部失败
kubectl create clusterrolebinding rest-op-anonymous --clusterrole=cluster-admin --user=system:anonymous
#添加admin角色后，无需token or client.crt即可访问
kubectl get clusterrolebinding
{
  "kind": "Status",
  "apiVersion": "v1",
  "metadata": {

  },
  "status": "Failure",
  "message": "pods is forbidden: User \"system:anonymous\" cannot list resource \"pods\" in API group \"\" in the namespace \"rest-op\"",
  "reason": "Forbidden",
  "details": {
    "kind": "pods"
  },
  "code": 403
}

#查询namespace=rest-op下的pod（成功
curl -k --cert ./client-cert.pem --key ./client-key.pem https://localhost:51004/api/v1/namespaces/rest-op/pods 
curl -k  https://localhost:51004/api/v1/namespaces/rest-op/pods 
curl -k --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/pods 

#创建pod（成功
curl -k -X POST --cert client-cert.pem --key client-key.pem https://localhost:51004/api/v1/namespaces/rest-op/pods -H 'Content-Type: application/yaml' --data-binary @nginx.yaml  --insecure
curl -k -X POST --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/pods -H 'Content-Type: application/yaml' --data-binary @nginx.yaml  --insecure

#curl nginx服务（成功
master：kubectl get pods --all-namespaces -o wide | grep nginx
edge：curl 192.168.1.3:80

#删除pod（成功
curl -k -X DELETE  --cert client-cert.pem --key client-key.pem  https://localhost:51004/api/v1/namespaces/rest-op/pods/
curl -k -X DELETE  --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/pods/
```

### 部署nginx pod服务（lite-apiserver，kube-apiserver断开连接

> 断开连接方案

```
# 断连：在master上打开防火墙，拒绝对6443端口访问
ufw enable

# 重连：支持对kube-apiserver访问，则所有请求恢复正常
ufw allow 6443 
```

![image-20220307195227129](https://tva1.sinaimg.cn/large/e6c9d24ely1h01kauphzwj20sy0c2ab2.jpg)

```shell
# bearer方式 or cert方式均可

#匿名用户权限问题（否则下面操作全部失败
kubectl create clusterrolebinding rest-op-anonymous --clusterrole=cluster-admin --user=system:anonymous
kubectl get clusterrolebinding
{
  "kind": "Status",
  "apiVersion": "v1",
  "metadata": {

  },
  "status": "Failure",
  "message": "pods is forbidden: User \"system:anonymous\" cannot list resource \"pods\" in API group \"\" in the namespace \"rest-op\"",
  "reason": "Forbidden",
  "details": {
    "kind": "pods"
  },
  "code": 403
}

#查询namespace=rest-op下的pod（失败
curl -k --cert client-cert.pem --key client-key.pem https://localhost:51004/api/v1/namespaces/rest-op/pods 
curl -k --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/pods 

#创建pod（失败
curl -k -X POST --cert client-cert.pem --key client-key.pem https://localhost:51004/api/v1/namespaces/rest-op/pods -H 'Content-Type: application/yaml' --data-binary @nginx.yaml  --insecure
curl -k -X POST --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/pods -H 'Content-Type: application/yaml' --data-binary @nginx.yaml  --insecure

#删除pod（失败
curl -k -X DELETE  --cert client-cert.pem --key client-key.pem  https://localhost:51004/api/v1/namespaces/rest-op/pods/rest-nginx
curl -k -X DELETE  --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/pods/rest-nginx
```

### 部署nginx service服务（lite-apiserver，kube-apiserver保持连接

```shell
# bearer方式 or cert方式均可

#匿名用户权限问题（否则下面操作全部失败
kubectl create clusterrolebinding rest-op-anonymous --clusterrole=cluster-admin --user=system:anonymous
kubectl get clusterrolebinding
{
  "kind": "Status",
  "apiVersion": "v1",
  "metadata": {

  },
  "status": "Failure",
  "message": "pods is forbidden: User \"system:anonymous\" cannot list resource \"pods\" in API group \"\" in the namespace \"rest-op\"",
  "reason": "Forbidden",
  "details": {
    "kind": "pods"
  },
  "code": 403
}

#查询namespace=rest-op下的service（成功
curl -k --cert client-cert.pem --key client-key.pem https://localhost:51004/api/v1/namespaces/rest-op/services
curl -k --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/services

#创建service（成功
curl -k -X POST --cert client-cert.pem --key client-key.pem https://localhost:51004/api/v1/namespaces/rest-op/services -H 'Content-Type: application/yaml' --data-binary @nginx-service.yaml  --insecure
curl -k -X POST --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/services -H 'Content-Type: application/yaml' --data-binary @nginx.yaml  --insecure

#curl nginx服务（成功
master：kubectl get service -A -o wide
edge：curl 10.105.73.159:88

#删除service（成功
curl -k -X DELETE  --cert client-cert.pem --key client-key.pem  https://localhost:51004/api/v1/namespaces/rest-op/services/rest-nginx-service
curl -k -X DELETE  --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/services/rest-nginx-service
```

### 部署nginx service服务（lite-apiserver，kube-apiserver断开连接

> 断开连接方案

```
# 断连：在master上打开防火墙，拒绝对6443端口访问
ufw enable

# 重连：支持对kube-apiserver访问，则所有请求恢复正常
ufw allow 6443 
```

```shell
# bearer方式 or cert方式均可

#匿名用户权限问题（否则下面操作全部失败
kubectl create clusterrolebinding rest-op-anonymous --clusterrole=cluster-admin --user=system:anonymous
kubectl get clusterrolebinding
{
  "kind": "Status",
  "apiVersion": "v1",
  "metadata": {

  },
  "status": "Failure",
  "message": "pods is forbidden: User \"system:anonymous\" cannot list resource \"pods\" in API group \"\" in the namespace \"rest-op\"",
  "reason": "Forbidden",
  "details": {
    "kind": "pods"
  },
  "code": 403
}

#查询namespace=rest-op下的service（失败
curl -k --cert client-cert.pem --key client-key.pem https://localhost:51004/api/v1/namespaces/rest-op/services
curl -k --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/services

#创建service（失败
curl -k -X POST --cert client-cert.pem --key client-key.pem https://localhost:51004/api/v1/namespaces/rest-op/services -H 'Content-Type: application/yaml' --data-binary @nginx-service.yaml  --insecure
curl -k -X POST --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/services -H 'Content-Type: application/yaml' --data-binary @nginx.yaml  --insecure

#删除service（失败
curl -k -X DELETE  --cert client-cert.pem --key client-key.pem  https://localhost:51004/api/v1/namespaces/rest-op/services/rest-nginx-service
curl -k -X DELETE  --header "Authorization: Bearer $TOKEN" https://localhost:51004/api/v1/namespaces/rest-op/services/rest-nginx-service
```

### 部署nginx deployment服务（lite-apiserver，kube-apiserver保持连接

```shell
# bearer方式 or cert方式均可

#匿名用户权限问题（否则下面操作全部失败
kubectl create clusterrolebinding rest-op-anonymous --clusterrole=cluster-admin --user=system:anonymous
kubectl get clusterrolebinding
{
  "kind": "Status",
  "apiVersion": "v1",
  "metadata": {

  },
  "status": "Failure",
  "message": "pods is forbidden: User \"system:anonymous\" cannot list resource \"pods\" in API group \"\" in the namespace \"rest-op\"",
  "reason": "Forbidden",
  "details": {
    "kind": "pods"
  },
  "code": 403
}

#查询namespace=rest-op下的deployment（成功
curl -k --cert client-cert.pem --key client-key.pem https://localhost:51004/apis/apps/v1/namespaces/rest-op/deployments
curl -k --header "Authorization: Bearer $TOKEN" https://localhost:51004/apis/apps/v1/namespaces/rest-op/deployments


#创建deployment（成功
curl -k -X POST --cert client-cert.pem --key client-key.pem https://localhost:51004/apis/apps/v1/namespaces/rest-op/deployments -H 'Content-Type: application/yaml' --data-binary @nginx-deployment.yaml  --insecure
curl -k -X POST --header "Authorization: Bearer $TOKEN" https://localhost:51004/apis/apps/v1/namespaces/rest-op/deployments -H 'Content-Type: application/yaml' --data-binary @nginx-deployment.yaml  --insecure

#curl nginx服务（成功
master：kubectl get pod -A -o wide
edge：curl 192.168.1.15:80

#删除deployment（成功
curl -k -X DELETE  --cert client-cert.pem --key client-key.pem  https://localhost:51004/apis/apps/v1/namespaces/rest-op/deployments/rest-nginx-deploy
curl -k -X DELETE  --header "Authorization: Bearer $TOKEN" https://localhost:51004/apis/apps/v1/namespaces/rest-op/deployments/rest-nginx-deploy
```

### 部署nginx deployment服务（lite-apiserver，kube-apiserver断开连接

- 失败，同pod、service

