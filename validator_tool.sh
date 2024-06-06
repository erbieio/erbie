#!/bin/bash
noderpcurl="http://127.0.0.1:8545"
nodekeypwd=".erbie/erbie/nodekey"

echo """
***********************************************************************
Notice:

Default value in this script should  modifided base on your situation:
node rpc url；noderpcurl="http://127.0.0.1:8545"
node file:    nodekeypwd=".erbie/erbie/nodekey"
**********************************************************************
"""

echo "Please choose and enter the number:"
echo

select opt in "to be a validator" "do not to be a validator" "displays the corresponding address based on the private key" "exit"
do
  case $opt in
    "to be a validator")
       #read -p "Enter your private key：" ky
      PASS=""
      echo -e "Enter your private key: \c"
        while : ;do
                char=` #这里是反引号，tab键上面那个
                        stty cbreak -echo
                        dd if=/dev/tty bs=1 count=1 2>/dev/null
                        stty -cbreak echo
                ` #这里是反引号，tab键上面那个
                if [ "$char" =  "" ];then
                        echo  #这里的echo只是为换行
                        break
                fi
                PASS="$PASS$char"
                echo -n "*"
	done

       if [ -n "$PASS" ]; then
          if [ ${#PASS} -ne 64 ];then
                #echo $PASS 
          #else
                echo "the private key format is not correct"
                #break
          else
      
      
      #nodekey=$(cat .erbie/erbie/nodekey)
      nodekey=$(cat $nodekeypwd)
      #echo $nodekey

      ./erbvalidator -cmd 1 -nodeurl $noderpcurl -prikey $PASS -proxykey $nodekey 

      address=$( ./erbvalidator -cmd 3 -nodeurl $noderpcurl  -prikey $PASS | awk '{print $3}')
      echo "private key address is: "$address
      echo "waitting for Verify operation results ..."
      sleep 30
      result=$(curl -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","method":"eth_getAccountInfo","params":["'$address'", "latest"],"id":50888}' $noderpcurl 2>/dev/null |grep -i $address)
                 if [ -n "$result" ];then
		          echo "validator!"
                 else
	      	          echo "not validator!"
                 fi
          fi
      fi
      #break
      ;;
    "do not to be a validator")
      #echo "do not to be a validator"
      # 在这里添加操作B的代码
      PASS=""
      echo -e "Enter your private key: \c"
        while : ;do
                char=` #这里是反引号，tab键上面那个
                        stty cbreak -echo
                        dd if=/dev/tty bs=1 count=1 2>/dev/null
                        stty -cbreak echo
                ` #这里是反引号，tab键上面那个
                if [ "$char" =  "" ];then
                        echo  #这里的echo只是为换行
                        break
                fi
                PASS="$PASS$char"
                echo -n "*"
        done

       if [ -n "$PASS" ]; then
          if [ ${#PASS} -ne 64 ];then
                echo "the private key format is not correct"
                
          else
      

    ./erbvalidator -cmd 2 -nodeurl $noderpcurl -prikey $PASS 

      address=$( ./erbvalidator -cmd 3 -nodeurl $noderpcurl  -prikey $PASS | awk '{print $3}')
      echo "private key address is: "$address
      echo "waitting for Verify operation results ..."
      sleep 30

      result=$(curl -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","method":"eth_getAccountInfo","params":["'$address'", "latest"],"id":51888}' $noderpcurl 2>/dev/null |grep -i $address)
                if [ -n "$result" ];then
                   echo "validator!"
                else
                   echo "not validator!"
                fi
         fi
      fi


      #break
      ;;
    "displays the corresponding address based on the private key")
      #echo "displays the corresponding address based on the private key"
      # 在这里添加操作C的代码

      PASS=""
      echo -e "Enter your private key: \c"
        while : ;do
                char=` #这里是反引号，tab键上面那个
                        stty cbreak -echo
                        dd if=/dev/tty bs=1 count=1 2>/dev/null
                        stty -cbreak echo
                ` #这里是反引号，tab键上面那个
                if [ "$char" =  "" ];then
                        echo  #这里的echo只是为换行
                        break
                fi
                PASS="$PASS$char"
                echo -n "*"
        done

       if [ -n "$PASS" ]; then
          if [ ${#PASS} -ne 64 ];then
                #echo $PASS 
          
                echo "the private key format is not correct"
                #break
	  else
      

    #./erbvalidator -cmd 3 -nodeurl "http://127.0.0.1:8545" -prikey $PASS
      address=$( ./erbvalidator -cmd 3 -nodeurl "http://127.0.0.1:8545" -prikey $PASS | awk '{print $3}')
      echo "private key address is: "$address
          fi
      fi
      #break
      ;;
    "exit")
      echo "exit"
      break
      ;;
    *) echo "Invalid option $REPLY";;
  esac
done
