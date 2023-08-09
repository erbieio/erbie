#!/bin/bash
supervisorctl stop erbie_log
supervisorctl start erbie
echo "block logs no show"
