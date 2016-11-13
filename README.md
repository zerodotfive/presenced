# Presenced

Presenced is a services registrator(like kube2sky for k8s) which registers services in etcd. It can be used with skydns or confd.

Presenced can check services state with TCP or HTTP requests and automaticaly unregister node on fail.
For the maintenance, when presenced gets SIGINT or SIGTERM, it sets node weight to 0.
