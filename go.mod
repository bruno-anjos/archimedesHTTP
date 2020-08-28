module github.com/bruno-anjos/archimedesHTTPClient

go 1.13

require (
	github.com/bruno-anjos/cloud-edge-deployment v0.0.1
	github.com/google/uuid v1.1.1
	github.com/sirupsen/logrus v1.6.0
)

replace github.com/bruno-anjos/cloud-edge-deployment v0.0.1 => ./../cloud-edge-deployment
