package main

import (
	"errors"
	"testing"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	kapi "k8s.io/kubernetes/pkg/api"
)

func makeTestService(name string) *kapi.Service {
	return &kapi.Service{
		ObjectMeta: kapi.ObjectMeta{
			Name:      name,
			Namespace: "bar",
		},
		Spec: kapi.ServiceSpec{
			Type:      kapi.ServiceTypeNodePort,
			ClusterIP: "192.168.1.1",
			Ports: []kapi.ServicePort{
				kapi.ServicePort{
					Name:     "tcp",
					Protocol: kapi.ProtocolTCP,
					Port:     80,
					NodePort: 8080,
				},
			},
		},
	}
}

func makeDefaultTestService() *kapi.Service {
	return makeTestService("Default")
}

//Copied from consul api tests
type configCallback func(c *consulapi.Config)

func makeClient(t *testing.T) (*consulapi.Client, *testutil.TestServer) {
	return makeClientWithConfig(t, nil, nil)
}

func makeClientWithConfig(
	t *testing.T,
	cb1 configCallback,
	cb2 testutil.ServerConfigCallback) (*consulapi.Client, *testutil.TestServer) {

	// Make client config
	conf := consulapi.DefaultConfig()
	if cb1 != nil {
		cb1(conf)
	}

	// Create server
	server := testutil.NewTestServerConfig(t, cb2)
	conf.Address = server.HTTPAddr

	// Create client
	client, err := consulapi.NewClient(conf)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	return client, server
}

func makeTestAgentWorker(t *testing.T) (*consulapi.Client, *ConsulAgentWorker, *testutil.TestServer) {
	c, s := makeClient(t)
	w := NewConsulAgentWorker(c)

	return c, w, s
}

func makeTestCatalogWorker(t *testing.T) (*consulapi.Client, *ConsulCatalogWorker, *testutil.TestServer) {
	c, s := makeClient(t)
	w := NewConsulCatalogWorker(c, makeCatalogConfig())

	return c, w, s
}

func makeDNSInfo() DNSInfo {
	return DNSInfo{
		BaseID:    "Foo",
		IPAddress: "192.168.1.1",
	}
}

func makeCatalogConfig() *ConsulWorkerConfig {
	return &ConsulWorkerConfig{
		TCPDomain: "test",
	}
}

func waitForServiceCount(t *testing.T, catalog *consulapi.Catalog, serviceName string, count int) {
	testutil.WaitForResult(func() (bool, error) {
		x, _, err := catalog.Service(serviceName, "", nil)

		if err != nil {
			return false, err
		}

		if len(x) != count {
			return false, errors.New("Wrong service count")
		}

		return true, nil
	}, func(err error) {
		t.Fatal("Error: ", err)
	})
}

func TestAgentAddDNS(t *testing.T) {
	c, w, s := makeTestAgentWorker(t)
	defer s.Stop()

	dns := makeDNSInfo()

	if fail := w.AddDNS(dns, makeDefaultTestService()); fail != nil {
		t.Fatal("AddDNS failed. ", fail)
	}

	testutil.WaitForResult(func() (bool, error) {
		catalog := c.Catalog()
		x, _, err := catalog.Services(nil)

		if err != nil {
			return false, err
		}
		for x := range x {
			print("Service:", x, "\n")
		}

		return true, nil
	}, func(err error) {
		t.Fatal("Error: ", err)
	})
}

func TestCatalogAddDNS(t *testing.T) {
	c, w, s := makeTestCatalogWorker(t)
	defer s.Stop()

	dns := makeDNSInfo()
	service := makeDefaultTestService()

	if fail := w.AddDNS(dns, service); fail != nil {
		t.Fatal("AddDNS failed. ", fail)
	}

	waitForServiceCount(t, c.Catalog(), service.Name, 1)

}

func TestCatalogRemoveDNS(t *testing.T) {
	c, w, s := makeTestCatalogWorker(t)
	defer s.Stop()

	dns := makeDNSInfo()
	service := makeDefaultTestService()

	if fail := w.AddDNS(dns, service); fail != nil {
		t.Fatal("AddDNS failed. ", fail)
	}

	waitForServiceCount(t, c.Catalog(), service.Name, 1)

	w.RemoveDNS(dns)
	waitForServiceCount(t, c.Catalog(), service.Name, 0)
}

func TestCatalogMultipleDNS(t *testing.T) {
	c, w, s := makeTestCatalogWorker(t)
	defer s.Stop()

	dns1 := makeDNSInfo()
	service := makeDefaultTestService()

	if fail := w.AddDNS(dns1, service); fail != nil {
		t.Fatal("AddDNS failed. ", fail)
	}

	dns2 := makeDNSInfo()
	dns2.BaseID = "testing2"

	if fail := w.AddDNS(dns2, service); fail != nil {
		t.Fatal("AddDNS failed on 2. ", fail)
	}

	waitForServiceCount(t, c.Catalog(), service.Name, 1)

	w.RemoveDNS(dns1)
	waitForServiceCount(t, c.Catalog(), service.Name, 1)

	w.RemoveDNS(dns2)
	waitForServiceCount(t, c.Catalog(), service.Name, 0)

}

func TestCatalogPurgeDNS(t *testing.T) {
	c, w, s := makeTestCatalogWorker(t)
	defer s.Stop()

	dns := makeDNSInfo()
	service := makeDefaultTestService()

	if fail := w.AddDNS(dns, service); fail != nil {
		t.Fatal("AddDNS failed. ", fail)
	}

	dns.BaseID = "testing2"
	service2 := makeTestService("Bar")

	if fail := w.AddDNS(dns, service2); fail != nil {
		t.Fatal("AddDNS failed. ", fail)
	}

	waitForServiceCount(t, c.Catalog(), service.Name, 1)
	waitForServiceCount(t, c.Catalog(), service2.Name, 1)

	w.PurgeDNS()
	waitForServiceCount(t, c.Catalog(), service.Name, 0)
	waitForServiceCount(t, c.Catalog(), service2.Name, 0)
}
