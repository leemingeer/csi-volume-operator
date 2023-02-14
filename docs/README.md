# example
1. create snapshot
2. write data2
3. create volumesnapshotrollback 
4. check rollback result
```
$ kubectl exec -it nginx1-6897745446-gsx96 bash
$ cd /var/www && dd if=/dev/zero of=/var/www/data1 bs=1M count=50
$ k apply -f snapscreate.yaml
$ k get volumesnapshot
snapshot-1          true         arstorplugin-pvc1                           10Gi          csi-arstor-snapclass   snapcontent-22e75ffd-710d-4da6-9782-7c2d2722baa4   75s            4m29s

$ kubectl exec -it nginx1-6897745446-gsx96 bash
$ cd /var/www && dd if=/dev/zero of=/var/www/data2 bs=1M count=30
$ ls
data1  data2  lost+found
$ k delete deploy nginx1
$  k apply -f config/samples/snapshotrollback_v1beta1_volumesnapshotrollback.yaml

$ k get volumesnapshotrollback
NAME                              PROCESS       AGE
volumesnapshotrollback-sample-1   Complete   9s
$ k apply nginx1.yaml

$ kubectl exec -it nginx1-6897745446-s68b6 bash
$ cd /var/www && ls -all
# ls -all
total 51220
drwxr-xr-x 3 root root     4096 Feb 15 03:14 .
drwxr-xr-x 1 root root       30 Feb 16 01:42 ..
drwx------ 2 root root    16384 Feb 15 03:04 lost+found
-rw-r--r-- 1 root root 52428800 Feb 15 03:14 data1
```