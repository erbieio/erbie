#!/bin/bash
#check docker cmd
echo "Script version Number: v0.14.4"
which docker >/dev/null 2>&1
if  [ $? -ne 0 ] ; then
        echo "docker not found, please install first!"
        echo "ubuntu:sudo apt install docker.io -y"
        echo "centos:yum install  -y docker-ce "
        echo "fedora:sudo dnf  install -y docker-ce"
        exit
fi
#check docker service
docker ps > /dev/null 2>&1
if [ $? -ne 0 ] ; then

        echo "docker service is not running! you can use command start it:"
        echo "sudo service docker start"
        exit
fi

vt5=1670383155
vl=$(wget https://docker.erbie.io/version>/dev/null 2>&1 && cat version|awk '{print $1}')
vr=$(cat version|awk '{print $2}' && rm version)
erb=$(docker images|grep "erbie/erbie"|grep "v1")
if [ -n "$erb" ];then
        ct=$(docker inspect erbie/erbie:v1 -f {{.Created}})
        cts=$(date -d "$ct" +%s)
        if [ $cts -eq $vl ];then
                container=$(docker ps -a|awk '{if($NF == "erbie")print $0}')
                if [[ $container =~ "Up" ]];then
                        while true
                        do
                                key=$(docker exec -it erbie /usr/bin/ls -l /erb/.erbie/erbie/nodekey)
                                if [ -n "$key" ];then
                                        echo -e "It is the latest version: $vr \nYour private key:"
                                        docker exec -it erbie /usr/bin/cat /erb/.erbie/erbie/nodekey
                                        echo -e "\n"
                                        exit 0
                                else
                                        sleep 5s
                                fi
                        done
                elif [[ $container =~ "Exited" ]];then
                        echo -e "Your peer isn't running\nYou can use 'docker start erbie' to start the node"
                        exit 0
                else
                        docker rm erbie
                        read -p "Enter your private key：" ky
                fi
        elif [ $cts -lt $vl ];then
                docker stop erbie > /dev/null 2>&1
                docker rm erbie > /dev/null 2>&1
                docker rmi erbie/erbie:v1 > /dev/null 2>&1
                if [ $cts -lt $vt5 ];then
                        if [ -f /erb/.erbie/erbie/nodekey ];then
                                echo "Clearing historical data ............"
                                cp /erb/.erbie/erbie/nodekey /erb/nodekey
                                rm -rf /erb/.erbie
                                mkdir -p /erb/.erbie/erbie
                                mv /erb/nodekey /erb/.erbie/erbie/
                        else
                                read -p "Enter your private key：" ky
                        fi
                elif [ $cts -ge $vt5 ];then
                        if [ ! -f /erb/.erbie/erbie/nodekey ];then
                                read -p "Enter your private key：" ky
                        fi
                fi
        fi
else
        read -p "Enter your private key：" ky
fi

if [ -n "$ky" ]; then
        mkdir -p /erb/.erbie/erbie
        if [ ${#ky} -eq 64 ];then
                echo $ky > /erb/.erbie/erbie/nodekey
        elif [ ${#ky} -eq 66 ] && ([ ${ky:0:2} == "0x" ] || [ ${ky:0:2} == "0X" ]);then
                echo ${ky:2:64} > /erb/.erbie/erbie/nodekey
        else
                echo "the nodekey format is not correct"
                exit 1
        fi
fi

docker pull erbie/erbie:v1

docker images|awk '{print $1,$2}'|while read img ver
do
        if [[ $img == "erbie/erbie" ]] && [[ $ver == "v1" ]];then
                echo "true">temp
                break
        fi
done

if [[ -f temp ]];then
        rm temp
        echo -e "\n\033[32mdocker pull images complete. Start running the container, please wait\033[0m\n"
else
        echo -e "\033[31mdocker pull err, please check your network\033[0m"
        exit 1
fi

docker run -id -p 30303:30303 -p 8545:8545 -v /erb/.erbie:/erb/.erbie --name erbie erbie/erbie:v1

while true
do
	echo  -e "running the container...\n"
        s=$(docker ps -a|grep "Up"|awk '{if($NF == "erbie") print $NF}'|wc -l)
        key=$(docker exec -it erbie /usr/bin/ls -l /erb/.erbie/erbie/nodekey 2>/dev/null)
        if [[ $s -gt 0 ]] && [[ "$key" =~ "nodekey" ]];then
                echo "Your private key is:"
                docker exec -it erbie /usr/bin/cat /erb/.erbie/erbie/nodekey
                echo -ne "\n"
                docker exec -it erbie ./erbie version|grep "Version"|grep -v go
                break
        else
                sleep 5s
        fi
done
