import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as autoscaling from 'aws-cdk-lib/aws-autoscaling';
import * as logs from 'aws-cdk-lib/aws-logs';
import * as s3 from 'aws-cdk-lib/aws-s3';
import * as elbv2 from 'aws-cdk-lib/aws-elasticloadbalancingv2';
import { Construct } from 'constructs';

export class ClickHouseStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    // VPC with public/private subnets
    const vpc = new ec2.Vpc(this, 'ClickHouseVpc', {
      maxAzs: 2,
      natGateways: 1, // Cost optimization: single NAT gateway
      subnetConfiguration: [
        {
          cidrMask: 24,
          name: 'Public',
          subnetType: ec2.SubnetType.PUBLIC,
        },
        {
          cidrMask: 24,
          name: 'Private',
          subnetType: ec2.SubnetType.PRIVATE_WITH_EGRESS,
        }
      ]
    });

    // S3 bucket for cold data storage
    const dataBucket = new s3.Bucket(this, 'ClickHouseDataBucket', {
      bucketName: `clickhouse-data-${this.account}-${this.region}`,
      lifecycleRules: [{
        id: 'transition-to-ia',
        enabled: true,
        transitions: [{
          storageClass: s3.StorageClass.INFREQUENT_ACCESS,
          transitionAfter: cdk.Duration.days(30)
        }, {
          storageClass: s3.StorageClass.GLACIER,
          transitionAfter: cdk.Duration.days(90)
        }]
      }],
      removalPolicy: cdk.RemovalPolicy.DESTROY // For dev - change for prod
    });

    // Security Group
    const clickhouseSecurityGroup = new ec2.SecurityGroup(this, 'ClickHouseSG', {
      vpc,
      description: 'Security group for ClickHouse server',
      allowAllOutbound: true
    });

    // Allow ClickHouse HTTP port (8123) and native port (9000)
    clickhouseSecurityGroup.addIngressRule(
      ec2.Peer.ipv4(vpc.vpcCidrBlock),
      ec2.Port.tcp(8123),
      'ClickHouse HTTP interface'
    );

    clickhouseSecurityGroup.addIngressRule(
      ec2.Peer.ipv4(vpc.vpcCidrBlock),
      ec2.Port.tcp(9000),
      'ClickHouse native interface'
    );

    // SSH access (optional - remove for production)
    clickhouseSecurityGroup.addIngressRule(
      ec2.Peer.anyIpv4(),
      ec2.Port.tcp(22),
      'SSH access'
    );

    // IAM role for ClickHouse instance
    const clickhouseRole = new iam.Role(this, 'ClickHouseRole', {
      assumedBy: new iam.ServicePrincipal('ec2.amazonaws.com'),
      managedPolicies: [
        iam.ManagedPolicy.fromAwsManagedPolicyName('CloudWatchAgentServerPolicy')
      ]
    });

    // Grant S3 access for cold storage
    dataBucket.grantReadWrite(clickhouseRole);

    // User data script to install and configure ClickHouse
    const userData = ec2.UserData.forLinux();
    userData.addCommands(
      '#!/bin/bash',
      'yum update -y',
      'yum install -y docker',
      'systemctl start docker',
      'systemctl enable docker',
      'usermod -a -G docker ec2-user',
      
      // Install CloudWatch agent
      'wget https://s3.amazonaws.com/amazoncloudwatch-agent/amazon_linux/amd64/latest/amazon-cloudwatch-agent.rpm',
      'rpm -U ./amazon-cloudwatch-agent.rpm',
      
      // Enhanced user data for persistent data handling
      'mkdir -p /opt/clickhouse/data',
      'mkdir -p /opt/clickhouse/logs',
      'mkdir -p /opt/clickhouse/config',
      
      // Mount or restore data from S3 if needed
      'aws configure set default.region ' + this.region,
      
      // Check if data exists in S3 and restore if needed
      'if aws s3 ls s3://' + dataBucket.bucketName + '/backups/latest/ > /dev/null 2>&1; then',
      '  echo "Restoring ClickHouse data from S3..."',
      '  aws s3 sync s3://' + dataBucket.bucketName + '/backups/latest/ /opt/clickhouse/data/',
      'fi',
      
      // Create ClickHouse configuration
      'cat > /opt/clickhouse/config/config.xml << EOF',
      '<?xml version="1.0"?>',
      '<clickhouse>',
      '    <logger>',
      '        <level>information</level>',
      '        <log>/var/log/clickhouse-server/clickhouse-server.log</log>',
      '        <errorlog>/var/log/clickhouse-server/clickhouse-server.err.log</errorlog>',
      '        <size>1000M</size>',
      '        <count>10</count>',
      '    </logger>',
      '    <http_port>8123</http_port>',
      '    <tcp_port>9000</tcp_port>',
      '    <listen_host>::</listen_host>',
      '    <max_connections>4096</max_connections>',
      '    <keep_alive_timeout>3</keep_alive_timeout>',
      '    <max_concurrent_queries>100</max_concurrent_queries>',
      '    <uncompressed_cache_size>8589934592</uncompressed_cache_size>',
      '    <mark_cache_size>5368709120</mark_cache_size>',
      '    <path>/var/lib/clickhouse/</path>',
      '    <tmp_path>/var/lib/clickhouse/tmp/</tmp_path>',
      '    <users_config>users.xml</users_config>',
      '    <default_profile>default</default_profile>',
      '    <default_database>default</default_database>',
      '    <timezone>UTC</timezone>',
      '    <mlock_executable>false</mlock_executable>',
      '    <storage_configuration>',
      '        <disks>',
      '            <s3>',
      '                <type>s3</type>',
      `                <endpoint>https://s3.${this.region}.amazonaws.com/${dataBucket.bucketName}/</endpoint>`,
      '                <use_environment_credentials>true</use_environment_credentials>',
      '            </s3>',
      '        </disks>',
      '        <policies>',
      '            <cold_storage>',
      '                <volumes>',
      '                    <hot>',
      '                        <disk>default</disk>',
      '                    </hot>',
      '                    <cold>',
      '                        <disk>s3</disk>',
      '                        <perform_ttl_move_on_insert>0</perform_ttl_move_on_insert>',
      '                    </cold>',
      '                </volumes>',
      '                <move_factor>0.1</move_factor>',
      '            </cold_storage>',
      '        </policies>',
      '    </storage_configuration>',
      '</clickhouse>',
      'EOF',
      
      // Create users configuration
      'cat > /opt/clickhouse/config/users.xml << EOF',
      '<?xml version="1.0"?>',
      '<clickhouse>',
      '    <profiles>',
      '        <default>',
      '            <max_memory_usage>10000000000</max_memory_usage>',
      '            <use_uncompressed_cache>0</use_uncompressed_cache>',
      '            <load_balancing>random</load_balancing>',
      '        </default>',
      '    </profiles>',
      '    <users>',
      '        <default>',
      '            <password></password>',
      '            <networks incl="networks_config" />',
      '            <profile>default</profile>',
      '            <quota>default</quota>',
      '        </default>',
      '    </users>',
      '    <quotas>',
      '        <default>',
      '            <interval>',
      '                <duration>3600</duration>',
      '                <queries>0</queries>',
      '                <errors>0</errors>',
      '                <result_rows>0</result_rows>',
      '                <read_rows>0</read_rows>',
      '                <execution_time>0</execution_time>',
      '            </interval>',
      '        </default>',
      '    </quotas>',
      '</clickhouse>',
      'EOF',
      
      // Start ClickHouse container
      'docker run -d \\',
      '  --name clickhouse-server \\',
      '  --restart unless-stopped \\',
      '  -p 8123:8123 \\',
      '  -p 9000:9000 \\',
      '  -v /opt/clickhouse/data:/var/lib/clickhouse \\',
      '  -v /opt/clickhouse/logs:/var/log/clickhouse-server \\',
      '  -v /opt/clickhouse/config/config.xml:/etc/clickhouse-server/config.xml \\',
      '  -v /opt/clickhouse/config/users.xml:/etc/clickhouse-server/users.xml \\',
      '  --ulimit nofile=262144:262144 \\',
      '  clickhouse/clickhouse-server:latest',
      
      // Setup log forwarding to CloudWatch
      'cat > /opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json << EOF',
      '{',
      '  "logs": {',
      '    "logs_collected": {',
      '      "files": {',
      '        "collect_list": [',
      '          {',
      '            "file_path": "/opt/clickhouse/logs/clickhouse-server.log",',
      '            "log_group_name": "/aws/ec2/clickhouse",',
      '            "log_stream_name": "{instance_id}-server"',
      '          }',
      '        ]',
      '      }',
      '    }',
      '  }',
      '}',
      'EOF',
      
      '/opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl \\',
      '  -a fetch-config \\',
      '  -m ec2 \\',
      '  -c file:/opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json \\',
      '  -s'
    );

    // CloudWatch Log Group
    const logGroup = new logs.LogGroup(this, 'ClickHouseLogGroup', {
      logGroupName: '/aws/ec2/clickhouse',
      retention: logs.RetentionDays.ONE_MONTH, // Cost optimization
      removalPolicy: cdk.RemovalPolicy.DESTROY
    });

    // Launch Template for Spot Instance
    const launchTemplate = new ec2.LaunchTemplate(this, 'ClickHouseLaunchTemplate', {
      instanceType: ec2.InstanceType.of(ec2.InstanceClass.M5, ec2.InstanceSize.LARGE),
      machineImage: ec2.MachineImage.latestAmazonLinux2(),
      securityGroup: clickhouseSecurityGroup,
      role: clickhouseRole,
      userData,
      blockDevices: [{
        deviceName: '/dev/xvda',
        volume: ec2.BlockDeviceVolume.ebs(50, {
          volumeType: ec2.EbsDeviceVolumeType.GP3,
          encrypted: true,
          deleteOnTermination: true
        })
      }],
      spotOptions: {
        requestType: ec2.SpotRequestType.ONE_TIME,
        interruptionBehavior: ec2.SpotInstanceInterruption.TERMINATE,
        maxPrice: 0.05 // Adjust based on current spot pricing
      }
    });

    // Auto Scaling Group for Spot Instance with persistent data
    const asg = new autoscaling.AutoScalingGroup(this, 'ClickHouseASG', {
      vpc,
      launchTemplate,
      minCapacity: 1,
      maxCapacity: 1,
      desiredCapacity: 1,
      vpcSubnets: {
        subnetType: ec2.SubnetType.PRIVATE_WITH_EGRESS
      },
      newInstancesProtectedFromScaleIn: true // Protect from accidental termination
    });

    // Application Load Balancer (optional - for external access)
    const alb = new elbv2.ApplicationLoadBalancer(this, 'ClickHouseALB', {
      vpc,
      internetFacing: true,
      securityGroup: new ec2.SecurityGroup(this, 'ALBSG', {
        vpc,
        allowAllOutbound: true
      })
    });

    // Allow HTTP traffic to ALB
    alb.connections.allowFromAnyIpv4(ec2.Port.tcp(80), 'Allow HTTP');

    // Target group for ClickHouse HTTP interface
    const targetGroup = new elbv2.ApplicationTargetGroup(this, 'ClickHouseTargetGroup', {
      vpc,
      port: 8123,
      protocol: elbv2.ApplicationProtocol.HTTP,
      targetType: elbv2.TargetType.INSTANCE,
      healthCheck: {
        path: '/ping',
        protocol: elbv2.Protocol.HTTP,
        port: '8123'
      }
    });

    // Attach ASG to target group
    asg.attachToApplicationTargetGroup(targetGroup);

    // Add listener
    alb.addListener('HTTPListener', {
      port: 80,
      protocol: elbv2.ApplicationProtocol.HTTP,
      defaultTargetGroups: [targetGroup]
    });

    // Outputs
    new cdk.CfnOutput(this, 'ClickHouseEndpoint', {
      value: `http://${alb.loadBalancerDnsName}`,
      description: 'ClickHouse HTTP endpoint'
    });

    new cdk.CfnOutput(this, 'AutoScalingGroup', {
      value: asg.autoScalingGroupName,
      description: 'ClickHouse Auto Scaling Group name'
    });

    new cdk.CfnOutput(this, 'S3Bucket', {
      value: dataBucket.bucketName,
      description: 'S3 bucket for cold data storage'
    });
  }
}
