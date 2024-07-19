workers=( 10.0.0.91 10.0.0.218 10.0.0.160 10.0.0.110 10.0.0.189
10.0.0.225 10.0.0.159 10.0.0.153 10.0.0.168
)

for worker in "${workers[@]}" ; do
    ssh ubuntu@"$worker" "sudo mkdir /etc/containerd/certs.d/_default"
    ssh ubuntu@"$worker" "sudo chmod -R 777 /etc/containerd/"
    sudo scp -i ~/.ssh/id_rsa config.toml ubuntu@"$worker":/etc/containerd/config.toml
    ssh ubuntu@"$worker" "sudo rm -rf /etc/containerd/_default"
    sudo scp -i ~/.ssh/id_rsa hosts.toml ubuntu@"$worker":/etc/containerd/certs.d/_default/hosts.toml
    ssh ubuntu@"$worker" "sudo systemctl restart containerd"
done