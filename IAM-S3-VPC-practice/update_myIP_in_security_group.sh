#!/bin/bash
# sh .\update_security_group_ip.sh <your-profile-name> <security-group-id> <region>
profile=$1
groupId=$2
region=$3
ipAddress=$(dig @resolver1.opendns.com ANY myip.opendns.com +short) 
aws ec2 authorize-security-group-ingress --group-id $groupId --protocol tcp --port 22 --cidr $ipAddress/32 --profile $profile --region $region

