package main

import (
	"bytes"
	"flag"
	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/s3"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"text/template"
)

type EC2Helper struct {
	client *ec2.EC2
}

func (h *EC2Helper) getInstanceById(instanceId string) (*ec2.Instance, error) {
	params := &ec2.DescribeInstancesInput{
		InstanceIDs: []*string{
			aws.String(instanceId),
		},
	}
	resp, err := h.client.DescribeInstances(params)

	if err != nil {
		return nil, err
	}

	reservation := resp.Reservations[0]
	instance := reservation.Instances[0]

	return instance, nil
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
	debug := flag.Bool("debug", false, "debug mode")

	flag.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
	}

	log.Debugf("Bucket: %v", *bucketName)
	log.Debugf("Prefix format: %v", *prefixFormat)
	log.Debugf("Create destination directory automatically?: %v", *createDirectory)

	var awsLogLevel uint
	if *debug {
		awsLogLevel = 1
	} else {
		awsLogLevel = 0
	}

	awsConfig := aws.Config{
		LogLevel: awsLogLevel,
	}

	ec2 := ec2.New(&awsConfig)
	ec2Helper := EC2Helper{
		client: ec2,
	}

	s3 := s3.New(&awsConfig)
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

	instance, err := ec2Helper.getInstanceById(*instanceId)
	if err != nil {
		log.Fatal(err)
	}

	tags := map[string]string{}
	for _, tag := range instance.Tags {
		tags[*tag.Key] = *tag.Value
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

	log.Debugf("%d objects", len(objects))

	// download to temp dir
	re := regexp.MustCompile("^" + regexp.QuoteMeta(prefix))
	for _, object := range objects {
		if *object.Size == 0 {
			continue
		}

		// remove prefix
		destPath := re.ReplaceAllString(*object.Key, "/")
		log.Debugf("%v -> %v", *object.Key, destPath)

		// determine temp path
		tempPath := path.Join(os.TempDir(), "confit-temp")

		// download
		data, err := s3Helper.getObject(*bucketName, *object.Key)
		if err != nil {
			log.Fatal(err)
		}

		dataBody, err := ioutil.ReadAll(data.Body)
		if err != nil {
			log.Fatal(err)
		}

		log.Debug("Writing to temporary file...")

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

		log.Debug("Moving temporary file to destination path...")

		err = os.Rename(tempPath, destPath)
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Info("Finished.")
}
