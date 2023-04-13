import json
import logging
import os

import boto3

logger = logging.getLogger()
logger.setLevel(logging.INFO)


# create boto3 client for SQS and SNS
sqs_client = boto3.client("sqs")
sns_client = boto3.client("sns")


def poll_sns_and_send_to_sns(sqs_queue_url: str, sns_topic_arn: str):
    # receive messages from SQS queue in batches
    while True:
        response = sqs_client.receive_message(
            QueueUrl=sqs_queue_url,
            MaxNumberOfMessages=3,
            WaitTimeSeconds=3,
        )

        if "Messages" in response:
            # extract the message body and delete the message from the queue
            message_body = [
                json.loads(message["Body"]) for message in response["Messages"]
            ]
            message_body = json.dumps(message_body, indent=4)
            sns_client.publish(TopicArn=sns_topic_arn, Message=message_body)
            logger.info(" Result message = \n" + message_body)
            for message in response["Messages"]:
                sqs_client.delete_message(
                    QueueUrl=sqs_queue_url, ReceiptHandle=message["ReceiptHandle"]
                )
        else:
            break


def lambda_handler(event, context):
    request_source = event.get("detail-type", event.get("resource", "null"))
    request_source = f"Request Resource: {request_source}"
    logger.info(request_source)

    sqs_queue_url = os.environ["SQS_QUEUE_URL"]
    sns_topic_arn = os.environ["SNS_TOPIC_ARN"]

    poll_sns_and_send_to_sns(sqs_queue_url, sns_topic_arn)

    return {"statusCode": 200, "body": request_source}
