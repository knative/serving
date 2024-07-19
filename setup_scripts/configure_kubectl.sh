
servers=( 10.0.0.123 10.0.0.91 10.0.0.218 10.0.0.160 10.0.0.110 10.0.0.189
10.0.0.225 10.0.0.159 10.0.0.153 10.0.0.168
)

workers=( 10.0.0.91 10.0.0.218 10.0.0.160 10.0.0.110 10.0.0.189
10.0.0.225 10.0.0.159 10.0.0.153 10.0.0.168
)
rm ~/.kube/config

for server in "${servers[@]}" ; do
    ssh ubuntu@"$server" "sudo mkdir -p ~/.kube"
    ssh ubuntu@"$server" "sudo rm ~/.kube/config"
done

ssh ubuntu@10.0.0.123 "sudo scp /etc/kubernetes/admin.conf ~/.kube/config"

for server in "${servers[@]}" ; do
    ssh ubuntu@"$server" "sudo chmod 644 ~/.kube/config"
done


sudo scp -i ~/.ssh/id_rsa ubuntu@10.0.0.123:~/.kube/config ~/.kube/config
sudo sed -i 's|server: .*|server: https://10.0.0.123:6443|' ~/.kube/config

for worker in "${workers[@]}" ; do
    sudo scp -i ~/.ssh/id_rsa ~/.kube/config ubuntu@"worker":~/.kube/config
done