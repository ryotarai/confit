package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"text/template"
	"time"

	"encoding/json"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/s3"
)

var version = "0.1.5"
var tagsCacheFilePrefix = "/tmp/confit-instance-tags-cache"

type EC2Helper struct {
	client   *ec2.EC2
	cacheTTL time.Duration
}

func (h *EC2Helper) getInstanceTags(instanceId string) (map[string]string, error) {
	file := fmt.Sprintf("%s.%s", tagsCacheFilePrefix, instanceId)
	info, err := os.Stat(file)
	if err == nil {
		d := time.Now().Sub(info.ModTime())
		if d <= h.cacheTTL {
			log.Info("Using cache")
			j, err := ioutil.ReadFile(file)
			if err != nil {
				return nil, err
			}

			tags := map[string]string{}
			err = json.Unmarshal(j, &tags)
			if err != nil {
				return nil, err
			}
			return tags, nil
		}
	}

	tags, err := h.getInstanceTagsWithoutCache(instanceId)
	if err != nil {
		return nil, err
	}

	b, err := json.Marshal(tags)
	if err != nil {
		return nil, err
	}

	err = ioutil.WriteFile(file, b, 0600)
	if err != nil {
		return nil, err
	}

	return tags, nil
}

func (h *EC2Helper) getInstanceTagsWithoutCache(instanceId string) (map[string]string, error) {
	params := &ec2.DescribeInstancesInput{
		InstanceIds: []*string{
			aws.String(instanceId),
		},
	}
	resp, err := h.client.DescribeInstances(params)

	if err != nil {
		return nil, err
	}

	reservation := resp.Reservations[0]
	instance := reservation.Instances[0]

	tags := map[string]string{}
	for _, t := range instance.Tags {
		tags[*t.Key] = *t.Value
	}
	return tags, nil
}

type S3Helper struct {
	client *s3.S3
}

func (h *S3Helper) listObjects(bucket string, prefix string) ([]*s3.Object, error) {
	log.Debug(bucket)
	log.Debug(prefix)

	params := &s3.ListObjectsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}
	resp, err := h.client.ListObjects(params)

	if err != nil {
		return nil, err
	}

	log.Debug(resp)
	return resp.Contents, nil
}

func (h *S3Helper) getObject(bucket string, key string) (*s3.GetObjectOutput, error) {
	params := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	resp, err := h.client.GetObject(params)

	if err != nil {
		return nil, err
	}

	return resp, nil
}

func main() {
	// load config
	bucketName := flag.String("bucket", "", "bucket name")
	prefixFormat := flag.String("prefix", "", "key prefix")
	createDirectory := flag.Bool("create-directory", true, "create destination directory automatically?")
	instanceId := flag.String("debug-instance-id", "", "instance id (for debug)")
	cacheTTLStr := flag.String("cache-ttl", "0s", "TTL for instance tags cache")
	debug := flag.Bool("debug", false, "debug mode")

	flag.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
	}

	cacheTTL, err := time.ParseDuration(*cacheTTLStr)
	if err != nil {
		log.Fatal(err)
	}

	log.Infof("Starting Confit v%v", version)
	log.Debugf("Bucket: %v", *bucketName)
	log.Debugf("Prefix format: %v", *prefixFormat)
	log.Debugf("Create destination directory automatically?: %v", *createDirectory)

	var awsLogLevel aws.LogLevelType
	if *debug {
		awsLogLevel = aws.LogDebug
	} else {
		awsLogLevel = aws.LogOff
	}

	awsConfig := aws.Config{
		LogLevel: &awsLogLevel,
	}

	sess := session.Must(session.NewSession(&awsConfig))

	ec2 := ec2.New(sess)
	ec2Helper := EC2Helper{
		client:   ec2,
		cacheTTL: cacheTTL,
	}

	s3 := s3.New(sess)
	s3Helper := S3Helper{
		client: s3,
	}

	// describe me
	if *instanceId == "" {
		log.Debug("Getting instance id...")
		resp, err := http.Get("http://169.254.169.254/latest/meta-data/instance-id")
		if err != nil {
			log.Fatal(err)
		}

		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}

		tmp := string(b)
		instanceId = &tmp
	}
	log.Debugf("Instance ID is %v", *instanceId)

	tags, err := ec2Helper.getInstanceTags(*instanceId)
	if err != nil {
		log.Fatal(err)
	}

	// render prefix
	tmpl, err := template.New("prefix").Parse(*prefixFormat)
	if err != nil {
		log.Fatal(err)
	}

	buf := new(bytes.Buffer)
	err = tmpl.Execute(buf, tags)
	if err != nil {
		log.Fatal(err)
	}

	prefix := buf.String()

	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	log.Debugf("Prefix: %v", prefix)

	// list files
	objects, err := s3Helper.listObjects(*bucketName, prefix)
	if err != nil {
		log.Fatal(err)
	}

	log.Infof("%d objects found", len(objects))

	// download
	re := regexp.MustCompile("^" + regexp.QuoteMeta(prefix))
	for _, object := range objects {
		if *object.Size == 0 {
			continue
		}

		// remove prefix
		destPath := re.ReplaceAllString(*object.Key, "/")
		log.Debugf("%v -> %v", *object.Key, destPath)

		// download
		data, err := s3Helper.getObject(*bucketName, *object.Key)
		if err != nil {
			log.Fatal(err)
		}

		dataBody, err := ioutil.ReadAll(data.Body)
		if err != nil {
			log.Fatal(err)
		}

		tempPath := fmt.Sprintf("%v/.%v.%d.confit.tmp", path.Dir(destPath), path.Base(destPath), time.Now().UnixNano())
		log.Debugf("Writing to %v", tempPath)

		err = ioutil.WriteFile(tempPath, dataBody, 0600)
		if err != nil {
			log.Fatal(err)
		}

		if *createDirectory {
			destDir := path.Dir(destPath)
			log.Debug("Creating destination directory...")
			err = os.MkdirAll(destDir, 0755)
			if err != nil {
				log.Fatal(err)
			}
		}

		log.Debugf("Moving %v to %v", tempPath, destPath)

		err = os.Rename(tempPath, destPath)
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Info("Finished.")
}
