Confit
======

Download config files from S3

Installation
------------

```
$ go get github.com/ryotarai/confit
```

Usage
-----

Assuming these objects exist on S3:

```
s3://example-bucket
└── roles
    └── example-role
        └── etc
            └── my
                └── config
```

Run confit on EC2 instance:

```
$ export AWS_ACCESS_KEY_ID=...
$ export AWS_SECRET_ACCESS_KEY=...
$ confit -bucket=example-bucket -prefix 'roles/{{.Role}}/'
$ cat /etc/my/config
This is my config!
```

`{{.Role}}` in prefix option will be replaced with Role tag.

Use case
--------

Run Confit by cron:

```
* * * * * root AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... /usr/local/bin/confit -bucket=example-bucket -prefix 'roles/{{.Role}}/'
* * * * * root AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... /usr/local/bin/confit -bucket=example-bucket -prefix 'hosts/{{.Name}}/'
```

You can update config files on all servers just by updating files on S3!

What is "confit"?
-----------------

http://en.wikipedia.org/wiki/Confit
