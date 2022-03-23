package main

import (
	"errors"
	"fmt"
	"math/rand"
	"mzssh/pkg/sshutils"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	awsSession "github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	connect "github.com/aws/aws-sdk-go/service/ec2instanceconnect"
	"github.com/olekukonko/tablewriter"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var version = "0.0.1"

func main() {
	rand.Seed(time.Now().Unix())
	setupSignalHandlers()

	app := &cli.App{
		Name:    "mzssh",
		Usage:   "ec2 connect을 이용한 ec2 인스턴스에 연결 및 터널링",
		Version: version,
		Action:  run,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "region",
				Aliases: []string{"r"},
				EnvVars: []string{"AWS_REGION"},
				Value:   "ap-northeast-2",
			},
			&cli.StringFlag{
				Name:  "tag",
				Value: "role:bastion",
			},
			&cli.StringFlag{
				Name:    "instance-id",
				Aliases: []string{"i"},
				Usage:   "바로 ssh로 붙을 instance id",
				Value:   "",
			},
			&cli.StringFlag{
				Name:    "user",
				Aliases: []string{"u"},
				Usage:   "OS UserName",
				Value:   "ec2-user",
			},
			&cli.StringFlag{
				Name:    "tunnel",
				Aliases: []string{"t"},
				Usage:   "터널링 대상 호스트",
			},
			&cli.BoolFlag{
				Name:    "lists",
				Aliases: []string{"l"},
				Usage:   "instance 목록",
			},
			&cli.StringSliceFlag{
				Name:    "destination",
				Aliases: []string{"d"},
				Usage:   "bastion을 통해 ssh로 붙을instance-id .",
			},
			&cli.IntFlag{
				Name:    "port",
				Aliases: []string{"p"},
				Value:   22,
			},
			&cli.IntFlag{
				Name:    "local-port",
				Aliases: []string{"lp"},
				Usage:   "매핑할 로컬 포트, 기본적으로 터널 포트",
			},
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "debug information",
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)

	}
}

func setupSignalHandlers() {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\n즐퇴!")
		os.Exit(0)
	}()
}

func run(c *cli.Context) error {
	if c.Bool("debug") {
		log.SetLevel(log.DebugLevel)
	}

	var tagName string
	var tagValue string

	if parts := strings.Split(c.String("tag"), ":"); len(parts) == 2 {
		tagName = parts[0]
		tagValue = parts[1]
	} else {
		return fmt.Errorf("%s 은(는)올바른 태그 정의가 아닙니다. key:value 형태로 입력해주세요", c.String("tag"))
	}

	ec2Client := ec2Client(c.String("region"))
	connectClient := connectClient(c.String("region"))

	instanceID := c.String("instance-id")
	if instanceID == "" {
		var err error
		instanceID, err = resolveBastionInstanceID(ec2Client, tagName, tagValue)
		if err != nil {
			return err
		}
	}

	bastionAddr := fmt.Sprintf("%s@%s:%d", c.String("user"), instanceID, c.Int("port"))
	bastionEndpoint, err := sshutils.NewEC2Endpoint(bastionAddr, ec2Client, connectClient)
	if err != nil {
		return err
	}

	if tunnel := sshutils.NewEndpoint(c.String("tunnel")); tunnel.Host != "" {
		p := c.Int("local-port")
		if p == 0 {
			p = tunnel.Port
		}
		return sshutils.Tunnel(p, tunnel, bastionEndpoint)
	}

	if c.Bool("lists") {
		runningInstances, err := getInstances(ec2Client)

		if err != nil {
			return err
		}

		allInstances := [][]string{}
		for _, r := range runningInstances.Reservations {
			for _, i := range r.Instances {
				var tag_name string
				var is_bastion string
				for _, t := range i.Tags {
					if *t.Key == "Name" {
						tag_name = *t.Value
					}
				}

				for _, ro := range i.Tags {
					if *ro.Key == "role" {
						is_bastion = *ro.Value
					}
				}

				if i.PublicIpAddress == nil {
					i.PublicIpAddress = aws.String("-")
				}
				instance := []string{
					tag_name,
					is_bastion,
					*i.InstanceId,
					*i.InstanceType,
					*i.Placement.AvailabilityZone,
					*i.PrivateIpAddress,
					*i.PublicIpAddress,
					*i.State.Name,
				}
				allInstances = append(allInstances, instance)
			}
		}

		outputTbl(allInstances)
		os.Exit(0)
	}

	chain := []sshutils.EndpointIface{
		bastionEndpoint,
	}

	for _, ep := range c.StringSlice("destination") {
		destEndpoint, err := sshutils.NewEC2Endpoint(ep, ec2Client, connectClient)
		if err != nil {
			return err
		}
		destEndpoint.UsePrivate = true
		chain = append(chain, destEndpoint)
	}

	return sshutils.Connect(chain...)
}

func outputTbl(data [][]string) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"tag:Name", "isBastion?", "InstanceId", "InstanceType", "AZ", "PrivateIp", "PublicIp", "Status"})

	for _, value := range data {
		table.Append(value)
	}
	table.Render()
}

func getSpotRequestByTag(ec2Client *ec2.EC2, tagName, tagValue string) (*ec2.DescribeSpotInstanceRequestsOutput, error) {
	return ec2Client.DescribeSpotInstanceRequests(&ec2.DescribeSpotInstanceRequestsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:" + tagName),
				Values: aws.StringSlice([]string{tagValue}),
			},
			{
				Name:   aws.String("state"),
				Values: aws.StringSlice([]string{"active"}),
			},
			{
				Name:   aws.String("status-code"),
				Values: aws.StringSlice([]string{"fulfilled"}),
			},
		},
	})
}

func getInstanceByTag(ec2Client *ec2.EC2, tagName, tagValue string) (*ec2.DescribeInstancesOutput, error) {
	return ec2Client.DescribeInstances(&ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:" + tagName),
				Values: aws.StringSlice([]string{tagValue}),
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: aws.StringSlice([]string{"running"}),
			},
		},
	})
}

func getInstances(client *ec2.EC2) (*ec2.DescribeInstancesOutput, error) {
	result, err := client.DescribeInstances(&ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("instance-state-name"),
				Values: []*string{
					aws.String("running"),
				},
			},
		},
	})

	if err != nil {
		return nil, err
	}

	return result, err
}

func resolveBastionInstanceID(ec2Client *ec2.EC2, tagName, tagValue string) (string, error) {
	log.Info("bastion host를 찾는 중입니다.")
	siro, err := getSpotRequestByTag(ec2Client, tagName, tagValue)
	if err != nil {
		return "", err
	}

	if len(siro.SpotInstanceRequests) > 0 {
		return aws.StringValue(siro.SpotInstanceRequests[rand.Intn(len(siro.SpotInstanceRequests))].InstanceId), nil
	}

	dio, err := getInstanceByTag(ec2Client, tagName, tagValue)
	if err != nil {
		return "", err
	}

	if len(dio.Reservations) > 0 {
		res := dio.Reservations[rand.Intn(len(dio.Reservations))]
		return aws.StringValue(res.Instances[rand.Intn(len(res.Instances))].InstanceId), nil
	}

	return "", errors.New("우효한 bastion instances가 없습니다.")
}

func ec2Client(region string) *ec2.EC2 {
	sess, err := awsSession.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		log.Fatal(err)
	}

	return ec2.New(sess)
}

func connectClient(region string) *connect.EC2InstanceConnect {
	sess, err := awsSession.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		log.Fatal(err)
	}

	return connect.New(sess)
}
