package main

import (
	"bytes"
	"flag"
	"github.com/mitchellh/goamz/aws"
	"github.com/mitchellh/goamz/ec2"
	"github.com/mitchellh/goamz/s3"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"text/template"
)

func main() {
	auth, err := aws.EnvAuth()
	if err != nil {
		log.Fatal(err)
	}
	ec2 := ec2.New(auth, aws.APNortheast)
	s3 := s3.New(auth, aws.APNortheast)

	// load config
	bucketName := flag.String("bucket", "", "bucket name")
	prefixFormat := flag.String("prefix", "", "key prefix")
	createDirectory := flag.Bool("create-directory", true, "create destination directory automatically?")
	instanceId := flag.String("debug-instance-id", "", "instance id (for debug)")

	flag.Parse()

	log.Printf("Bucket: %v", *bucketName)
	log.Printf("Prefix format: %v", *prefixFormat)
	log.Printf("Create destination directory automatically?: %v", *createDirectory)

	// describe me
	if *instanceId == "" {
		log.Printf("Getting instance id...")
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
	log.Printf("Instance ID is %v", *instanceId)

	instancesResp, err := ec2.Instances([]string{*instanceId}, nil)
	if err != nil {
		log.Fatal(err)
	}
	reservation := instancesResp.Reservations[0]
	instance := reservation.Instances[0]

	log.Printf("Instance: %v", instance)

	tags := map[string]string{}
	for _, tag := range instance.Tags {
		tags[tag.Key] = tag.Value
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

	log.Printf("Prefix: %v", prefix)

	// list files
	bucket := s3.Bucket(*bucketName)
	listResp, err := bucket.List(prefix, "", "", 1000)
	if err != nil {
		log.Fatal(err)
	}

	// download to temp dir
	re := regexp.MustCompile("^" + regexp.QuoteMeta(prefix))
	for _, content := range listResp.Contents {
		if content.Size == 0 {
			continue
		}

		// remove prefix
		destPath := re.ReplaceAllString(content.Key, "/")
		log.Printf("%v -> %v", content.Key, destPath)

		// determine temp path
		tempPath := path.Join(os.TempDir(), "confit-temp")

		// download
		data, err := bucket.Get(content.Key)
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("Writing to temporary file...")

		err = ioutil.WriteFile(tempPath, data, 0600)
		if err != nil {
			log.Fatal(err)
		}

		if *createDirectory {
			destDir := path.Dir(destPath)
			log.Printf("Creating destination directory...")
			err = os.MkdirAll(destDir, 0700)
			if err != nil {
				log.Fatal(err)
			}
		}

		log.Printf("Moving...")

		err = os.Rename(tempPath, destPath)
		if err != nil {
			log.Fatal(err)
		}
	}
}
