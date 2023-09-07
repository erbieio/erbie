#!/bin/bash


read -p "Please input latest filename of go Archive, like go1.21.0.linux-amd64.tar.gz: " go_filename

wget https://go.dev/dl/${go_filename}
if [ $? -ne 0 ];then
    echo "wget failed, perhaps the go_filename is wrong"
    exit 1
fi
sudo rm -rf /usr/local/go
sudo tar -C /usr/local/ -zxvf ${go_filename}

sudo sed -i '$a export PATH=\$PATH:/usr/local/go/bin' /etc/profile
source /etc/profile

which git
if [ $? -ne 0 ];then
    sudo apt install git -y
fi
git clone https://github.com/erbieio/erbie.git
if [ $? -ne 0 ];then
    echo "git clone failed"
    exit 1
fi

cd erbie
make erbie

nohup ./build/bin/erbie --devnet --datadir .erbie --http --mine --syncmode=full --rpcport 8561 --port 30321 &
