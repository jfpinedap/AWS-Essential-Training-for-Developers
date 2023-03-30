import boto3
import os

# IMPORTANT This script is designed to work on EC2 instance with a role that grant s3 access

# Create an S3 client with the EC2 instance's role credentials
session = boto3.Session()
s3 = session.client('s3')

# Define the S3 bucket and folder you want to download
web_folder = 's3_web_exercise/'
bucket_name = 'demo-bucket-jfpinedap'
folder_name = f'test_folder/{web_folder}/'

# Define the local folder to download the files to
local_folder = f'./{web_folder}'

# Create the local folder if it doesn't exist
if not os.path.exists(local_folder):
    os.makedirs(local_folder)

# Download all files in the S3 folder recursively
paginator = s3.get_paginator('list_objects_v2')
result_iterator = paginator.paginate(Bucket=bucket_name, Prefix=folder_name)

for result in result_iterator:
    if 'Contents' in result:
        for item in result['Contents']:
            key = item['Key']
            local_file = os.path.join(local_folder, key[len(folder_name):])
            if not os.path.exists(os.path.dirname(local_file)):
                os.makedirs(os.path.dirname(local_file))
            s3.download_file(bucket_name, key, local_file)
