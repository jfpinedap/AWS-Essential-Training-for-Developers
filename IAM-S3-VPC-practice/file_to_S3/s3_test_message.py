import boto3
import botocore.credentials

# set up SSO authentication
session = boto3.Session(profile_name='FullAccessS3')
creds = session.get_credentials().get_frozen_credentials()

# set up S3 client
s3 = boto3.client('s3', 
                  region_name='us-east-1', 
                  aws_access_key_id=creds.access_key, 
                  aws_secret_access_key=creds.secret_key,
                  aws_session_token=creds.token)

# specify bucket and file paths
bucket_name = 's3-bucket-demo-159357'
file_path = 's3_test_message.txt'
s3_key = 'test_folder/s3_test_message.txt'

# upload file to S3
with open(file_path, 'rb') as f:
    s3.upload_fileobj(f, bucket_name, s3_key)

print(f'Successfully uploaded {file_path} to s3://{bucket_name}/{s3_key}')

