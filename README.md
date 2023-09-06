1. 改动了fork的处理高度 

```shell
curl -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","method":"eth_getPunishedInfo","params":["0xc"],"id":51888}' 127.0.0.1:8560
```

```shell
curl -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","method":"eth_getAccountInfo","params":["0xffac4cd934f026dcaf0f9d9eeddcd9af85d8943e", "latest"],"id":51888}' 127.0.0.1:8560
```
