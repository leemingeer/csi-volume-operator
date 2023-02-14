
# csi-volume-operator

csi-volume-operator aims to provide external features for volume manipulation based on standard csi controller plane sidecars.

the feature is composed of two parts: controller plane and the dataplane

this sidecar is a controller plane which controllers the crds and make call to dataplane.
the dataplane is your csi-driver in which you can define the way manipulating pvcs&&pv on your own.

currently we just realise simple features:
- rollback pvc by snapshot without creating new pv.
- concurrently rollback different pvc but serial rollback for same pvc.

It is flexible for developers by creating more CRs to do volume adaption!


# Compile

support Arch: amd64 and arm64

## build

```
make container-build ARCH=<arch> IMAGE_TAG=<image>
```
## buildx
```
make container-buildx ARCH=<arch> IMAGE_TAG=<image>
```

# Deploy && Use

add csi-volume-operator sidecar to spec.template.spec.container[] of csi-controller pod
```
     - args:
       - --csi-address=$(ADDRESS)
       env:
       - name: ADDRESS
         value: unix:///csi/csi.sock
       image: csi-volume-operator:v1
       imagePullPolicy: IfNotPresent
       name: csi-volume-operator
       resources: {}
       terminationMessagePath: /dev/termination-log
       terminationMessagePolicy: File
       volumeMounts:
       - mountPath: /csi
         name: socket-dir
```

## RBAC

use serviceaccount `system:serviceaccount:kube-system:csi-plugin` binded to the role which has related rights for volumesnapshotrollbacks and volumesnapshotrollbacks/status in snapshotrollback.storage.k8s.io API Group

```
kubectl apply -f config/rbac/role_binding.yaml
kubectl apply -f config/rbac/service_account.yaml
kubectl apply -f  config/rbac/volumesnapshotrollback_editor_role.yaml
kubectl apply -f  config/rbac/volumesnapshotrollback_viewer_role.yaml
```

## create crd and check progress
```
#kubectl apply -f config/samples/snapshotrollback_v1beta1_volumesnapshotrollback.yaml
$ k get volumesnapshotrollbacks -o wide
NAME                            PROCESS    AGE
volumesnapshotrollback-sample   Complete   36m
```
