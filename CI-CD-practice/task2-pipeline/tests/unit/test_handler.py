import json
from unittest import mock

import pytest


@pytest.fixture()
def apigw_event():
    """Generates API GW Event"""

    return {"resource": "/sam-uploads-batch-notifier"}


@mock.patch.dict(
    "os.environ", {"SQS_QUEUE_URL": "sqs_queue_url", "SNS_TOPIC_ARN": "sns_topic_arn"}
)
@mock.patch("src.app.poll_sns_and_send_to_sns")
def test_lambda_handler(mock_sns, apigw_event):
    from src import app

    ret = app.lambda_handler(apigw_event, "")
    data = json.loads(ret["body"])

    assert ret["statusCode"] == 200
    assert "message" in ret["body"]
    assert data["message"] == "Successful response"
    assert data["request_source"] == "Request Resource: /sam-uploads-batch-notifier"
    assert data["sqs_messages"] == []
