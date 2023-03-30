#!/bin/bash

aws s3api list-object-versions --bucket $1 --prefix $2 --query "Versions[?LastModified<=$3][].{version: VersionId, modified: LastModified}" --profile $4 --max-items 1
