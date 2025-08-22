#!/usr/bin/env node
import 'source-map-support/register';
import * as cdk from 'aws-cdk-lib';
import { ClickHouseStack } from '../lib/clickhouse-stack';

const app = new cdk.App();

new ClickHouseStack(app, 'ClickHouseStack', {
  env: {
    account: process.env.CDK_DEFAULT_ACCOUNT,
    region: process.env.CDK_DEFAULT_REGION || 'us-east-1',
  },
  
  // Optional: Add tags to all resources
  tags: {
    Project: 'ClickHouse',
    Environment: 'dev', // Change for different environments
    Owner: 'your-team'
  }
});

app.synth();

