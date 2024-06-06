# ErbieChain

The ErbieChain solves the blockchain trilemma, which entails a necessary tradeoff between scalability, security, and decentralization, by building the
technology to achieve the ideal balance between these three metrics, creating a highly scalable and secure blockchain system that doesn’t sacrifice
decentralization.

## The Approach

The significant step before spinning up your node is choosing your approach. Based on requirements and many potential possibilities,
you must select the client implementation (of both execution and consensus clients), the environment (hardware, system), and the
parameters for client settings.

To decide whether to run the software on your hardware or in the cloud depending on your demands.

You can use the startup script to start your node after preparing the environment.

When the node is running and syncing, you are ready to use it, but make sure to keep an eye on its maintenance.

## Environment and Hardware

Erbie clients are able to run on consumer-grade computers and do not require any special hardware, such as mining machines.
Therefore, you have more options for deploying the node based on your demands. let us think about running a node on both a local
physical machine and a cloud server:

### Hardware

Erbie clients can run on your computer, laptop, server, or even a single-board computer. Although running clients on
different devices are possible, it had better use a dedicated machine to enhance its performance and underpin the security,
which can minimize the impact on your computer.

Hardware requirements differ by the client but generally are not that high since the node just needs to stay synced.

Do not confuse it with mining, which requires much more computing power. However , sync time and performance do improve with more
powerful hardware.

### Minimum requirements

-  CPU: Main frequency 2.9GHz, 4 cores or above CPU.
-  Memory: Capacity 8GB or more.
-  Hard Disk: Capacity 500GB or more.
-  Network bandwidth: 6M uplink and downlink peer-to-peer rate or higher

Before installing the client, please ensure your computer has enough resources to run it. You can find the minimum and recommended requirements below.

## Spin-up Your Own Erbie Node

Participate in the Erbie blockchain public testnet, jointly support and maintain the Erbie network ecosystem, and you can obtain corresponding
benefits. 

This tutorial will guide you to deploy Erbie nodes and participate in verifying the security and reliability of the Erbie network. Choose the 
software tools and deployment methods you are familiar with to maintain your own nodes.

### Docker Clients Setup

#### Preparation

- Install wget. 

  Please go to the [wget website](https://www.gnu.org/software/wget/) to download and install it. If you are using Linux system, you can also 
install it using the `apt-get install wget` command. If you are using MacOS system, you can also install it using the `brew install wget` command.

- Install Docker.

  For the installation and use of Docker, please refer to the [Docker Official Documentation](https://docs.docker.com/engine/install/).

#### Run the node

When using the script to start the node, you must enter the private key of the account used for pledge prepared earlier. For details, see the
documentation [Deploy Erbie Nodes Using Official Scripts](https://www.erbie.io/docs/install/run/deploy/docker.html).

### Manual clients setup

The actual client setup can be done by using the automatic launcher or manually.

For ordinary users, we recommend you use a startup script, which guides you through the installation and automates the client setup process. However, if
you have experience with the terminal, the manual setup steps should be easy to follow.

#### Startup parameters

- Start ***Erbie*** in fast sync mode (default, can be changed withthe ***--syncmode*** flag),causing it to download more data in exchange for
avoiding processing the entire history of the Erbie Chain network, which is very CPU intensive.

- Start up ***Erbie's*** built-in interactive JavaScript,(via the trailing ***console*** subcommand) through which you can interact using ***web3***
  [methods](https://web3js.readthedocs.io/en/v1.2.9/)(note: the ***web3*** version bundled within ***Erbie*** is very old, and
  not up to date with official docs).
  This tool is optional and if you leave it out you can always attach to an already running ``Erbie`` instance with ***Erbie attach*** .

#### Full nodes functions

-  Stores the full blockchain history on disk and can answer the data request from the network.
-  Receives and validates the new blocks and transactions.
-  Verifies the states of every account.

#### Start validator node

1. Download the binary, config and genesis files from [release](https://github.com/erbieio/erbie.git), or compile the binary by ``make erbie``.

2. Start Erbie.

   **Running the following command starts Erbie. After successful launch, a private key will be generated automatically for you.**

	````
	./build/bin/erbie --mainnet --http --mine --syncmode=full
	````
   --http: This enables the http-rpc server that allows external programs to interact with Erbie by sending it http requests. By default the http server is only exposed locally using port 8545: localhost:8545.

There are then many combinations of commands that configure precisely how erbie will run. The same information can be obtained at any time from your Erbie instance by running:

````
./build/bin/erbie --help
```` 


