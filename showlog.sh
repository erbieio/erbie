#!/bin/bash
supervisorctl stop erbie
supervisorctl start erbie_log
echo "block logs will show in /erb/.erbie/node1/logs"
