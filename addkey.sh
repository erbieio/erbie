#!/bin/bash
cd
erbhome=".erbie/erbie"

#read -p "Enter your private key or press 'ENTER' for none:" ky
      echo -e "Enter your private key or press 'ENTER' for none: \c"
        while : ;do
                char=`
                        stty cbreak -echo
                        dd if=/dev/tty bs=1 count=1 2>/dev/null
                        stty -cbreak echo
                `
                if [ "$char" =  "" ];then
                        echo
                        break
                fi
                PASS="$PASS$char"
                echo -n "*"
	done
if [ -n "$PASS" ]; then
        mkdir -p $erbhome
        if [ ${#PASS} -eq 64 ];then

                echo "$PASS" > $erbhome/nodekey
        elif [ ${#PASS} -eq 66 ] && ([ ${PASS:0:2} == "0x" ] || [ ${PASS:0:2} == "0X" ]);then
                echo ${PASS:2:64} > $erbhome/nodekey
        else
                echo "the nodekey format is not correct"
                exit 1
        fi
fi