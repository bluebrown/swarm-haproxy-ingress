package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"text/template"

	"github.com/bluebrown/labelparser"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/mitchellh/mapstructure"
)

// create the config data from the docker client
func CreateConfigData(ctx context.Context, cli *client.Client) (ConfigData, error) {
	// initialize the a new config struct
	conf := ConfigData{}
	conf.Backend = make(map[string]Backend)

	// get the labels from the manager itself, as it holds global
	// and frontend configs
	info, err := cli.ContainerInspect(ctx, os.Getenv("HOSTNAME"))
	if err != nil {
		return conf, err
	}
	// create initial config data from the manager labels
	ParseManagerInfo(&conf, info)

	opts := types.ServiceListOptions{}
	// if an ingress class was specified, use it to filter the services
	if conf.IngressClass != "" {
		opts.Filters = filters.NewArgs(filters.KeyValuePair{Key: "label", Value: "ingress.class=" + conf.IngressClass})
	}
	// get the list of swarm services
	services, err := cli.ServiceList(ctx, opts)
	if err != nil {
		return conf, err
	}

	// parse the services
	ParseSwarmServices(&conf, services)

	// return the final config struct
	return conf, nil
}

// parse the manager labels into the config data
func ParseManagerInfo(conf *ConfigData, info types.ContainerJSON) {
	c := map[string]interface{}{}
	labelparser.Parse(info.Config.Labels, &c)

	// if it contains ingress rules decode them
	if val, ok := c["ingress"]; ok {
		mapstructure.Decode(val, &conf)
	}
}

// iterate through all services to merge their configs into
// the config data, created from the manager labels
func ParseSwarmServices(conf *ConfigData, services []swarm.Service) {
	for _, svc := range services {
		log.Printf("Parsing service %s...", svc.Spec.Name)

		// the backend name is the service name
		BackendName := svc.Spec.Name

		// get the service labels
		svcLabels := map[string]interface{}{}
		labelparser.Parse(svc.Spec.Labels, &svcLabels)

		// if it contains ingres rules decode them
		if ingressLabels, ok := svcLabels["ingress"]; ok {
			// get the backend config
			be := Backend{}
			mapstructure.Decode(ingressLabels, &be)

			// replicas are used from the service spec
			be.Replicas = *svc.Spec.Mode.Replicated.Replicas

			// if there are config snippets, merge them into the corresponding frontends
			// from the manager labels
			for name, snippet := range be.Frontend {
				if _, ok := conf.Frontend[name]; ok {
					// try to parse the snippet as template
					tmpl, err := template.New("backend").Parse(snippet)
					if err != nil {
						fmt.Printf("ERROR: failed to parse config snippet for backend %s: %v", be.Backend, err)
						continue
					}
					// execute the template and pass the backend name to it
					data := new(bytes.Buffer)
					tmpl.Execute(data, struct{ Name string }{Name: BackendName})
					// make sure the snippets ends with a new line
					stringData := data.String()
					if !strings.HasSuffix(stringData, "\n") {
						stringData += "\n"
					}
					// append snippet to the corresponding frontend
					conf.Frontend[name] += stringData
				} else {
					fmt.Println("WARNING: skipping frontend, name not found in manager labels: " + name)
				}
			}

			// add the backend to the config data
			conf.Backend[BackendName] = be
			log.Printf("Added backend: %s with port %s\n", BackendName, be.Port)
		}
	}
}