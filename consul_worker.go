package main // import "github.com/jmccarty3/kube2consul"

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/golang/glog"

	consulapi "github.com/hashicorp/consul/api"
	kapi "k8s.io/kubernetes/pkg/api"
)

//ConsulWorkerConfig is a configuration object for the consul worker
type ConsulWorkerConfig struct {
	TCPDomain string
}

//ConsulWorker Interface for interacting with a Consul object
type ConsulWorker interface {
	AddDNS(config DNSInfo, service *kapi.Service) error
	RemoveDNS(config DNSInfo) error
	SyncDNS()
	PurgeDNS()
}

//ConsulAgentWorker ConsulWorker with a connection to a Consul agent
type ConsulAgentWorker struct {
	ids   map[string][]*consulapi.AgentServiceRegistration
	agent *consulapi.Client
}

//ConsulCatalogWorker operates on consuls catalog rather then a direct agent
type ConsulCatalogWorker struct {
	domain   string
	agent    *consulapi.Client
	ids      map[string][]string //Keeps track of which services a "BaseID" is associated with
	services map[string]int      //Keeps track of how many "nodes" are attempting to use the service.
}

func isServiceNameValid(name string) bool {
	if strings.Contains(name, ".") == false {
		return true
	}

	glog.Infof("Names containing '.' are not supported: %s\n", name)
	return false
}

func isServiceValid(service *kapi.Service) bool {
	if isServiceNameValid(service.Name) {
		if kapi.IsServiceIPSet(service) {
			if service.Spec.Type == kapi.ServiceTypeNodePort {
				return true // Service is valid
			}
			//Currently this is only for NodePorts.
			glog.V(3).Infof("Skipping non-NodePort service: %s\n", service.Name)
		} else {
			// if ClusterIP is not set, do not create a DNS records
			glog.Infof("Skipping dns record for headless service: %s\n", service.Name)
		}
	}

	return false
}

func createAgentServiceCheck(config DNSInfo, port *kapi.ServicePort) *consulapi.AgentServiceCheck {
	glog.V(3).Info("Creating service check for: ", config.IPAddress, " on Port: ", port.NodePort)
	return &consulapi.AgentServiceCheck{
		TCP:      config.IPAddress + ":" + strconv.Itoa(int(port.NodePort)),
		Interval: "60s",
	}
}

func createServiceNameFromPort(serviceName string, port *kapi.ServicePort) string {
	var name string

	if len(port.Name) > 0 {
		name = serviceName + "-" + port.Name
	} else {
		name = serviceName + "-" + strconv.Itoa(int(port.Port))
	}

	return name
}

func createAgentServiceReg(config DNSInfo, name string, service *kapi.Service, port *kapi.ServicePort) *consulapi.AgentServiceRegistration {
	labels := []string{"Kube", string(port.Protocol)}
	asrID := config.BaseID + port.Name

	if name == "" {
		name = createServiceNameFromPort(service.Name, port)
	}

	return &consulapi.AgentServiceRegistration{
		ID:      asrID,
		Name:    name,
		Address: config.IPAddress,
		Port:    int(port.NodePort),
		Tags:    labels,
	}
}

func createAgentService(name string, service *kapi.Service, port *kapi.ServicePort) *consulapi.AgentService {
	labels := []string{"Kube", string(port.Protocol)}

	if name == "" {
		name = createServiceNameFromPort(service.Name, port)
	}

	return &consulapi.AgentService{
		Service: name,
		Tags:    labels,
	}
}

/*
func createCatalogRegistration(name string, service *kapi.Service, port *kapi.ServicePort) *consulapi.CatalogRegistration {
}
*/

//NewConsulAgentWorker Creates a new ConsulAgentWorker connected to a client
func NewConsulAgentWorker(client *consulapi.Client) *ConsulAgentWorker {
	return &ConsulAgentWorker{
		agent: client,
		ids:   make(map[string][]*consulapi.AgentServiceRegistration),
	}
}

//AddDNS Adds the DNS information to consul
func (client *ConsulAgentWorker) AddDNS(config DNSInfo, service *kapi.Service) error {
	glog.V(3).Info("Starting Add DNS for: ", config.BaseID)

	if config.IPAddress == "" || config.BaseID == "" {
		glog.Error("DNS Info is not valid for AddDNS")

		return errors.New("DNS Info invalid")
	}

	//Validate Service
	if !isServiceValid(service) {
		return errors.New("Service Not Valid")
	}
	//Check Port Count & Determine DNS Entry Name
	var serviceName string

	if len(service.Spec.Ports) == 1 {
		serviceName = service.Name
	} else {
		serviceName = ""
	}

	var failed []string

	for _, port := range service.Spec.Ports {
		asr := createAgentServiceReg(config, serviceName, service, &port)

		if *argChecks && port.Protocol == "TCP" {
			//Create Check if neeeded
			asr.Check = createAgentServiceCheck(config, &port)
		}

		if client.agent != nil {
			//Registers with DNS
			if err := client.agent.Agent().ServiceRegister(asr); err != nil {
				glog.Error("Error creating service record: ", asr.ID)
				failed = append(failed, asr.ID)
				continue
			}
		}

		//Add to IDS
		client.ids[config.BaseID] = append(client.ids[config.BaseID], asr)
	}

	if len(failed) != 0 {
		return fmt.Errorf("Error creating service: %s", failed)
	}
	//Exit
	return nil
}

//RemoveDNS Removes the DNS information requested from Consul
func (client *ConsulAgentWorker) RemoveDNS(config DNSInfo) error {
	if ids, ok := client.ids[config.BaseID]; ok {
		for _, asr := range ids {
			if client.agent != nil {
				if err := client.agent.Agent().ServiceDeregister(asr.ID); err != nil {
					glog.Error("Error removing service: ", err)
				}
			}
		}
		delete(client.ids, config.BaseID)
	} else {
		glog.Error("Requested to remove non-existant BaseID DNS of:", config.BaseID)
	}

	return nil
}

func containsServiceID(id string, services map[string]*consulapi.AgentService) bool {
	for _, service := range services {
		if service.ID == id {
			return true
		}
	}

	return false
}

//SyncDNS Verifies that all requested services are actually registered
func (client *ConsulAgentWorker) SyncDNS() {
	if client.agent != nil {
		if services, err := client.agent.Agent().Services(); err == nil {
			for _, registered := range client.ids {
				for _, service := range registered {
					if !containsServiceID(service.ID, services) {
						glog.Info("Regregistering missing service ID: ", service.ID)
						client.agent.Agent().ServiceRegister(service)
					}
				}
			}
		} else {
			glog.Info("Error retreiving services from consul during sync: ", err)
		}
	}
}

//PurgeDNS Removes all currently registered entries from consul
func (client *ConsulAgentWorker) PurgeDNS() {
	for id := range client.ids {
		dns := DNSInfo{
			BaseID: id,
		}

		client.RemoveDNS(dns)
	}
}

//NewConsulCatalogWorker creates a worker to act on consuls catalog
func NewConsulCatalogWorker(client *consulapi.Client, config *ConsulWorkerConfig) *ConsulCatalogWorker {
	return &ConsulCatalogWorker{
		agent:    client,
		domain:   config.TCPDomain,
		ids:      make(map[string][]string),
		services: make(map[string]int),
	}
}

//AddDNS Adds the service to consuls catalog
func (client *ConsulCatalogWorker) AddDNS(config DNSInfo, service *kapi.Service) error {
	//Validate Service
	if !isServiceValid(service) {
		return fmt.Errorf("Service %s is not vaild", service.Name)
	}

	//Check Port Count & Determine DNS Entry Name
	var serviceName string

	if len(service.Spec.Ports) == 1 {
		serviceName = service.Name
	} else {
		serviceName = ""
	}

	if client.services[service.Name] == 0 { //Only register if the service is new to us
		for _, port := range service.Spec.Ports {
			s := createAgentService(serviceName, service, &port)
			cr := &consulapi.CatalogRegistration{
				Node:    fmt.Sprintf("%s-kube2consul", s.Service),
				Address: fmt.Sprintf("%s.%s", s.Service, client.domain),
				Service: s,
			}

			if client.agent != nil {
				_, err := client.agent.Catalog().Register(cr, nil)
				if err != nil {
					return err
				}
			}
		}
	}

	client.ids[config.BaseID] = append(client.ids[config.BaseID], service.Name)
	client.services[service.Name] = client.services[service.Name] + 1

	return nil
}

//RemoveDNS Removes the dns information from consul
func (client *ConsulCatalogWorker) RemoveDNS(config DNSInfo) error {
	if services, exists := client.ids[config.BaseID]; exists {
		for s := range services { //TODO This logic needs to be split up. Way too much scope
			if count, ok := client.services[services[s]]; ok {
				count--

				if count <= 0 {
					if client.agent != nil {
						cdr := &consulapi.CatalogDeregistration{
							Node: fmt.Sprintf("%s-kube2consul", services[s]),
						}

						client.agent.Catalog().Deregister(cdr, nil)
					}
					delete(client.services, services[s])
				} else {
					client.services[services[s]] = count
				}
			}
		}
		delete(client.ids, config.BaseID)
	} else {
		glog.Warning("Attempted to remove unregistered service:", config.BaseID)
	}
	return nil
}

//SyncDNS Does nothing on the catalog
func (*ConsulCatalogWorker) SyncDNS() {
	//Nothing to sync
}

//PurgeDNS removes all currently registered items from consul
func (client *ConsulCatalogWorker) PurgeDNS() {
	for id := range client.ids {
		dns := DNSInfo{
			BaseID: id,
		}

		client.RemoveDNS(dns)
	}
}

func cleanup(c <-chan os.Signal, worker ConsulWorker) {
	<-c
	glog.Info("Trapped termination signal. Purging DNS")
	worker.PurgeDNS()
	glog.Info("DNS Purged. Exiting")
	os.Exit(0)
}

//RunConsulWorker Runs the ConsulWorker while the queue is open
func RunConsulWorker(queue <-chan ConsulWork, client *consulapi.Client, config ConsulWorkerConfig) {
	var worker ConsulWorker

	if config.TCPDomain == "" {
		worker = NewConsulAgentWorker(client)
	} else {
		worker = NewConsulCatalogWorker(client, &config)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go cleanup(sigs, worker)

	for work := range queue {
		glog.V(4).Info("Consol Work Action: ", work.Action, " BaseID:", work.Config.BaseID)

		switch work.Action {
		case ConsulWorkAddDNS:
			worker.AddDNS(work.Config, work.Service)
		case ConsulWorkRemoveDNS:
			worker.RemoveDNS(work.Config)
		case ConsulWorkSyncDNS:
			worker.SyncDNS()
		default:
			glog.Error("Unsupported Action of: ", work.Action)
		}

	}
}
