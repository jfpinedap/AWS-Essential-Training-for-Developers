import logging
import json

import watchtower

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)
logger.addHandler(watchtower.CloudWatchLogHandler(log_group_name="LogGroup_Lambda_Stack"))
logger.info("Hi")
logger.info(dict(foo="bar", details={}))

def lambda_handler(event, context):
  logger.info(json.dumps(event))

  return {
      'statusCode': 200,
      'body': json.dumps(event)
  }