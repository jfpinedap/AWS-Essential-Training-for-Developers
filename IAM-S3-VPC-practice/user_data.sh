#!/bin/bash
yum update -y
yum install -y httpd
systemctl start httpd.service
systemctl enable httpd.service
cd /var/www/html
aws s3 sync s3://demo-bucket-jfpinedap/test_folder/s3_web_exercise/ .
systemctl start httpd.service
systemctl enable httpd.service
