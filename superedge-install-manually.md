# 手动部署superedge

## 部署tunnel

### 部署tunnel-coredns

```
# 创建edge-system namespace
kubectl create namespace edge-system
```

```
# 部署tunnel coredns
kubectl apply -f deployment/tunnel-coredns.yaml
```

### 部署tunnel-cloud

- 生成tunnel的CA

```shell
# Generate CA private key
openssl genrsa -out tunnel-ca.key 2048

# Generate CSR
openssl req -new -key tunnel-ca.key -out tunnel-ca.csr

# Add DNS and IP
echo "subjectAltName=DNS:superedge.io,IP:127.0.0.1" > tunnel_ca_cert_extensions

# Generate Self Signed certificate
openssl x509 -req -days 365 -in tunnel-ca.csr -signkey tunnel-ca.key -extfile tunnel_ca_cert_extensions -out tunnel-ca.crt
```

- 生成TunnelPersistentConnectionServerKey和TunnelPersistentConnectionServerCrt

```shell
# private key
openssl genrsa -des3 -out tunnel_persistent_connectiong_server.key 2048

# generate csr
openssl req -new -key tunnel_persistent_connectiong_server.key -subj "/CN=tunnel-cloud" -out tunnel_persistent_connectiong_server.csr

# Add DNS and IP, 必须填写 "DNS:tunnelcloud.io"
echo "subjectAltName=DNS:tunnelcloud.io,IP:127.0.0.1" > tunnel_persistent_connectiong_server_cert_extensions

# Generate Self Signed certificate
openssl x509 -req -days 365 -in tunnel_persistent_connectiong_server.csr -CA tunnel-ca.crt -CAkey tunnel-ca.key -CAcreateserial  -extfile tunnel_persistent_connectiong_server_cert_extensions -out tunnel_persistent_connectiong_server.crt
```

- 生成TunnelProxyServerKey和TunnelProxyServerCrt（用于kube-apiserver和tunnel-cloud之间的认证）

```shell
# private key
openssl genrsa -des3 -out tunnel_proxy_server.key 2048

# generate csr
openssl req -new -key tunnel_proxy_server.key -subj "/CN=tunnel-cloud" -out tunnel_proxy_server.csr

# Add DNS and IP
echo "subjectAltName=DNS:superedge.io,IP:127.0.0.1" > cert_extensions

# Generate Self Signed certificate（注意ca.crt和ca.key为集群的证书, Kubeadm部署的集群中，CA是/etc/kubernetes/pki下的ca.crt和ca.key）
openssl x509 -req -days 365 -in tunnel_proxy_server.csr -CA ca.crt -CAkey ca.key -CAcreateserial  -extfile cert_extensions -out tunnel_proxy_server.crt
```

- 设置环境变量

```
export TunnelCloudEdgeToken=OIauTBIqmkRFN5xM7l1bLbpNeF1OsLVY
TunnelPersistentConnectionServerCrt=$(cat tunnel_persistent_connectiong_server.key | base64 --wrap=0)
TunnelPersistentConnectionServerKey=$(cat tunnel_persistent_connectiong_server.crt | base64 --wrap=0)
TunnelPersistentConnectionServerCrt=$(cat tunnel_proxy_server.key | base64 --wrap=0)
TunnelPersistentConnectionServerKey=$(cat tunnel_proxy_server.crt | base64 --wrap=0)

# 部署 deployment/tunnel-cloud.yaml
envsubst < deployment/tunnel-cloud.yaml | kubectl apply -f -
```

### kube-apiserver使用tunnel

```
# 修改kube-apierver的DNS，使用tunnel-coredns 修改为tunnel-cloud的CLUSTER-IP
dnsnameserver=$(kubectl get service tunnel-coredns -n edge-system | grep tunnel-coredns | awk '{print $3}')

cat << EOF > /etc/kubernetes/manifests/kube-apiserver.yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    kubeadm.kubernetes.io/kube-apiserver.advertise-address.endpoint: 192.168.92.100:6443
  creationTimestamp: null
  labels:
    component: kube-apiserver
    tier: control-plane
  name: kube-apiserver
  namespace: kube-system
spec:
  containers:
  - command:
    - kube-apiserver
    - --advertise-address=192.168.92.100
    - --allow-privileged=true
    - --authorization-mode=Node,RBAC
    - --client-ca-file=/etc/kubernetes/pki/ca.crt
    - --enable-admission-plugins=NodeRestriction
    - --enable-bootstrap-token-auth=true
    - --etcd-cafile=/etc/kubernetes/pki/etcd/ca.crt
    - --etcd-certfile=/etc/kubernetes/pki/apiserver-etcd-client.crt
    - --etcd-keyfile=/etc/kubernetes/pki/apiserver-etcd-client.key
    - --etcd-servers=https://127.0.0.1:2379
    - --insecure-port=0
    - --kubelet-client-certificate=/etc/kubernetes/pki/apiserver-kubelet-client.crt
    - --kubelet-client-key=/etc/kubernetes/pki/apiserver-kubelet-client.key
    - --kubelet-preferred-address-types=InternalIP,ExternalIP,Hostname
    - --proxy-client-cert-file=/etc/kubernetes/pki/front-proxy-client.crt
    - --proxy-client-key-file=/etc/kubernetes/pki/front-proxy-client.key
    - --requestheader-allowed-names=front-proxy-client
    - --requestheader-client-ca-file=/etc/kubernetes/pki/front-proxy-ca.crt
    - --requestheader-extra-headers-prefix=X-Remote-Extra-
    - --requestheader-group-headers=X-Remote-Group
    - --requestheader-username-headers=X-Remote-User
    - --secure-port=6443
    - --service-account-key-file=/etc/kubernetes/pki/sa.pub
    - --service-cluster-ip-range=10.96.0.0/12
    - --tls-cert-file=/etc/kubernetes/pki/apiserver.crt
    - --tls-private-key-file=/etc/kubernetes/pki/apiserver.key
    image: registry.cn-hangzhou.aliyuncs.com/google_containers/kube-apiserver:v1.18.2
    imagePullPolicy: IfNotPresent
    dnsConfig:
      nameservers:
        - ${dnsnameserver}
    livenessProbe:
      failureThreshold: 8
      httpGet:
        host: 192.168.92.100
        path: /healthz
        port: 6443
        scheme: HTTPS
      initialDelaySeconds: 15
      timeoutSeconds: 15
    name: kube-apiserver
    resources:
      requests:
        cpu: 250m
    volumeMounts:
    - mountPath: /etc/ssl/certs
      name: ca-certs
      readOnly: true
    - mountPath: /etc/pki
      name: etc-pki
      readOnly: true
    - mountPath: /etc/kubernetes/pki
      name: k8s-certs
      readOnly: true
  hostNetwork: true
  priorityClassName: system-cluster-critical
  volumes:
  - hostPath:
      path: /etc/ssl/certs
      type: DirectoryOrCreate
    name: ca-certs
  - hostPath:
      path: /etc/pki
      type: DirectoryOrCreate
    name: etc-pki
  - hostPath:
      path: /etc/kubernetes/pki
      type: DirectoryOrCreate
    name: k8s-certs
status: {}
EOF
```

### todo 部署tunnel-edge

- 将ca.crt kubelet_client.key kubelet_client.crt拷贝到边缘node /etc/superedge/tunnel/certs/
  - scp root@master:/root/go_project/superedge/cert/tunnel/*  /root/cert/

```
# private key
openssl genrsa -des3 -out kubelet_client.key 1024
# generate csr
openssl req -new -key kubelet_client.key -out kubelet_client.csr
# Generate Self Signed certificate（注意ca.crt和ca.key为集群的证书, Kubeadm部署的集群中，CA是/etc/kubernetes/pki下的ca.crt和ca.key）
https://blog.csdn.net/weixin_41979048/article/details/80374945
openssl ca -in kubelet_client.csr -out kubelet_client.crt -cert ca.crt -keyfile ca.key

openssl x509 -req -days 365 -in kubelet_client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out kubelet_client.crt

export TunnelCloudEdgeToken=OIauTBIqmkRFN5xM7l1bLbpNeF1OsLVY
export KubernetesCaCert=$(cat ca.key | base64 --wrap=0)
export KubeletClientCrt=$(cat kubelet_client.crt | base64 --wrap=0)
export KubeletClientKey=$(cat kubelet_client.key | base64 --wrap=0)

envsubst < deployment/tunnel-edge.yaml | kubectl apply -f -
```

## 部署lite-apiserver

> master节点

```
# 修改deployment/lite-apiserver.yaml中的–kube-apiserver-url和–kube-apiserver-port指向apiserver的host和port
cat << EOF > /root/go_project/superedge/deployment/lite-apiserver.yaml
---
apiVersion: v1
kind: Pod
metadata:
  labels:
    k8s-app: lite-apiserver
  name: lite-apiserver
  namespace: edge-system
spec:
  containers:
    - command:
        - lite-apiserver
        - --ca-file=/etc/kubernetes/pki/ca.crt
        - --tls-cert-file=/etc/kubernetes/edge/lite-apiserver.crt
        - --tls-private-key-file=/etc/kubernetes/edge/lite-apiserver.key
        - --kube-apiserver-url=192.168.92.100
        - --kube-apiserver-port=6443
        - --port=51003
        - --tls-config-file=/etc/kubernetes/edge/tls.json
        - --v=4
        - --file-cache-path=/data/lite-apiserver/cache
        - --timeout=3
      # image: superedge.tencentcloudcr.com/superedge/lite-apiserver:v0.1.0
      image: superedge/lite-apiserver:v0.1.0
      imagePullPolicy: IfNotPresent
      name: lite-apiserver
      volumeMounts:
        - mountPath: /etc/kubernetes/pki
          name: k8s-certs
        - mountPath: /etc/kubernetes/edge
          name: edge-certs
          readOnly: true
        - mountPath: /var/lib/kubelet/pki
          name: kubelet
          readOnly: true
        - mountPath: /data
          name: cache
  hostNetwork: true
  volumes:
    - hostPath:
        path: /var/lib/kubelet/pki
        type: DirectoryOrCreate
      name: kubelet
    - hostPath:
        path: /data
      name: cache
    - hostPath:
        path: /etc/kubernetes/pki
        type: DirectoryOrCreate
      name: k8s-certs
    - hostPath:
        path: /etc/kubernetes/edge
        type: DirectoryOrCreate
      name: edge-certs
status: {}
EOF

cd /root/go_project/superedge/cert/tunnel

openssl genrsa -out lite-apiserver.key 2048

clusterip=$(kubectl get service kubernetes | grep kubernetes | awk '{print $3}')

cat << EOF > lite-apiserver.conf
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
[req_distinguished_name]
CN = lite-apiserver
[v3_req]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
subjectAltName = @alt_names
[alt_names]
DNS.1 = localhost
IP.1 = 127.0.0.1
IP.2 = $clusterip # 请改成对应kubernetes的ClusterIP
EOF

openssl req -new -key lite-apiserver.key -subj "/CN=lite-apiserver" -config lite-apiserver.conf -out lite-apiserver.csr

openssl x509 -req -in lite-apiserver.csr -CA ca.crt -CAkey ca.key -CAcreateserial -days 5000 -extensions v3_req -extfile lite-apiserver.conf -out lite-apiserver.crt
```

> 边缘节点

- 分发lite-apiserver.crt和lite-apiserver.key到边缘节点的/etc/kubernetes/pki/和/etc/kubernetes/edge/下

  - scp root@master:/root/go_project/superedge/cert/tunnel/lite-*  /etc/kubernetes/pki/

  - scp root@master:/root/go_project/superedge/cert/tunnel/lite-apiserver* /etc/kubernetes/edge/ 

```shell
mkdir -p /etc/kubernetes/edge/
# 在边缘节点上创建/etc/kubernetes/edge/tls.json文件，写入如下内容
cat << EOF > /etc/kubernetes/edge/tls.json
[
    {
        "key":"/var/lib/kubelet/pki/kubelet-client-current.pem",
        "cert":"/var/lib/kubelet/pki/kubelet-client-current.pem"
    }
]
EOF
```

- 使用Static Pod方式将lite-apiserver部署在*边缘Node节点*中, 分发deployment/lite-apiserver.yaml到边缘kubelet的manifests下
  - scp root@master:/root/go_project/superedge/deployment/lite* /etc/kubernetes/manifests/

- 修改kubelet.conf中的cluster.server为 https://127.0.0.1:51003

```
cat << EOF > /etc/kubernetes/kubelet.conf
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUN5RENDQWJDZ0F3SUJBZ0lCQURBTkJna3Foa2lHOXcwQkFRc0ZBREFWTVJNd0VRWURWUVFERXdwcmRXSmwKY201bGRHVnpNQjRYRFRJeU1ETXlOVEEyTWpVME1Wb1hEVE15TURNeU1qQTJNalUwTVZvd0ZURVRNQkVHQTFVRQpBeE1LYTNWaVpYSnVaWFJsY3pDQ0FTSXdEUVlKS29aSWh2Y05BUUVCQlFBRGdnRVBBRENDQVFvQ2dnRUJBTmUyCmdsWDVWbFQ1U1RNK0MzcnVZOGdnMnBwbW9hS3hOb1I4QVBWekNpZFFUWE10TnpCV3I3Nko2K2RSaWlZdGxZOXoKNzJWRWZybVN0WmM4a0RwNEdvaWtRVHl6WE1qZmdQZ3F0MHcxUE9kaUVmamZNeUhvRkdBWERFVnhydkVqbzdoYgp3VGVSby9BcldDQ3MzUjU3RDJHS0JiMHExN2JET2xhTElJcVVTejFHWndPV1hNQTIyQkZqT1hCeXVJOHZVWHkrCnRuSVJ5czV2TGVhR1VQWUJ2RGh0VlNrZEsxbURuTTk2ZDZIcE5SOUpUVU9DcW8vdFhFL2lUVjJyRGI3YjFycUcKcjR5V2Nhdy9jeGJtckQrZFRxZEFJdlVGekt1L0ovamZZTmtlK25Mb2JDczR6YWx6VmVRTkdHU0xhMWdpbHlCdQpjMG9EOXdPT1paNXN1QjBzcHRjQ0F3RUFBYU1qTUNFd0RnWURWUjBQQVFIL0JBUURBZ0trTUE4R0ExVWRFd0VCCi93UUZNQU1CQWY4d0RRWUpLb1pJaHZjTkFRRUxCUUFEZ2dFQkFEUENTOU9nVlAvUXk2S1ZrNnZ6cnFXdXdNQWUKSEo2ZkYwN21PZHVMN2w0RmpGVXI2NWpHejhXc1NrVWZudFRaWVM5RnVFWnFRZ0F2MlJHdXBUaGRCTXI2RWdjVAprU1RkRnZ5UkJTV1haN0RiVXFsdzArQTFKVmw3eUhVU1Fyb0pZYU5OSHZ4RldUUFpzUHdkaVo2ZkwwTWFDMkVVCktENCthOUhvNzcyMWJUd0RhREJKeFd1QzZpZXBTTTF5UGJqaXhLOExMcURQbWkxbEZ2WWZNcVJKdXFRWU0rZlkKelh2VysxemlVVmFTYklCbUhVYjVkTDBrRkg3WnhITUhSQi9UZGZYNWZkc1JUS2JhQXA5aHVlWGhJR21QZkx0dQpLRllwRktvOE02VDN2Qm0welhFS0dncVh4Qm9ZYkFmMzVNbVY1QXgyeEJILzdweWs0RENqWEkzSEZnaz0KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
    server: https://127.0.0.1:51003
  name: default-cluster
contexts:
- context:
    cluster: default-cluster
    namespace: default
    user: default-auth
  name: default-context
current-context: default-context
kind: Config
preferences: {}
users:
- name: default-auth
  user:
    client-certificate: /var/lib/kubelet/pki/kubelet-client-current.pem
    client-key: /var/lib/kubelet/pki/kubelet-client-current.pem
EOF
```

## 部署Application Grid

> master节点上

```shell
# 使用Deployment方式，将application-grid-controller部署在云端control plane节点中
kubectl apply -f deployment/application-grid-controller.yaml

# Add Annotate Endpoint Kubernetes 让kubernestes endpoints通过lite-apiserver访问kube-apiserver
kubectl annotate endpoints kubernetes superedge.io/local-endpoint=127.0.0.1
kubectl annotate endpoints kubernetes superedge.io/local-port=51003

# 使用DaemonSet方式，将application-grid-wrapper部署在边缘Node节点中 Application-grid-wrapper通过lite-apiserver请求kube-apiserver
kubectl apply -f deployment/application-grid-wrapper.yaml

# 修改kube-proxy配置文件的cluster.server为 http://127.0.0.1:51006 （kube-proxy的配置文件位于kube-system namespace下的 kube-proxy的configMap中）
kubectl edit configmaps -n kube-system kube-proxy
```

## 部署Edge Health

> master节点上

```shell
# 使用Deployment方式，将edge-health-admission部署在云端control plane节点中
kubectl apply -f deployment/edge-health-admission.yaml

# 使用Deployment方式，将edge-health-webhook部署在云端control plane节点中
kubectl apply -f deployment/edge-health-webhook.yaml

#HmacKey：分布式健康检查，edge-health的消息验证key，至少16位随机字符串
hmackey=$(cat /dev/urandom | tr -dc '[:alnum:]' | head -c16)
# 使用DaemonSet方式，将edge-health部署在边缘Node节点中
kubectl apply -f deployment/edge-health.yaml
```

## Reference

https://superedge.io/zh/docs/installation/install-manually/