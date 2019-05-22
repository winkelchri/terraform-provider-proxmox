package proxmox

import (
	"crypto/tls"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"sync"

	pxapi "github.com/Telmate/proxmox-api-go/proxmox"
	"github.com/hashicorp/terraform/helper/schema"
)

type providerConfiguration struct {
	Client          *pxapi.Client
	MaxParallel     int
	CurrentParallel int
	MaxVMID         int
	Mutex           *sync.Mutex
	Cond            *sync.Cond
}

// Provider - Terrafrom properties for proxmox
func Provider() *schema.Provider {
	log.Print("[INFO] Provider()")
	return &schema.Provider{

		Schema: map[string]*schema.Schema{
			"pm_user": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("PM_USER", nil),
				Description: "username, maywith with @pam",
			},
			"pm_password": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("PM_PASS", nil),
				Description: "secret",
				Sensitive:   true,
			},
			"pm_api_url": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("PM_API_URL", nil),
				Description: "https://host.fqdn:8006/api2/json",
			},
			"pm_parallel": {
				Type:     schema.TypeInt,
				Optional: true,
				Default:  4,
			},
			"pm_tls_insecure": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
		},

		ResourcesMap: map[string]*schema.Resource{
			"proxmox_vm_qemu": resourceVmQemu(),
			// TODO - storage_iso
			// TODO - bridge
			// TODO - vm_qemu_template
		},

		ConfigureFunc: providerConfigure,
	}
}

func providerConfigure(d *schema.ResourceData) (interface{}, error) {
	log.Print("[INFO] providerConfigure()")
	client, err := getClient(
		d.Get("pm_api_url").(string),
		d.Get("pm_user").(string),
		d.Get("pm_password").(string),
		d.Get("pm_tls_insecure").(bool),
	)
	if err != nil {
		return nil, err
	}
	var mut sync.Mutex
	return &providerConfiguration{
		Client:          client,
		MaxParallel:     d.Get("pm_parallel").(int),
		CurrentParallel: 0,
		MaxVMID:         -1,
		Mutex:           &mut,
		Cond:            sync.NewCond(&mut),
	}, nil
}

func getClient(pm_api_url string, pm_user string, pm_password string, pm_tls_insecure bool) (*pxapi.Client, error) {
	log.Print("[INFO] getClient()")
	tlsconf := &tls.Config{InsecureSkipVerify: true}
	if !pm_tls_insecure {
		tlsconf = nil
	}
	client, _ := pxapi.NewClient(pm_api_url, nil, tlsconf)
	err := client.Login(pm_user, pm_password)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func nextVmId(pconf *providerConfiguration) (nextId int, err error) {
	log.Print("[INFO] nextVmId()")
	pconf.Mutex.Lock()
	pconf.MaxVMID, err = pconf.Client.GetNextID(pconf.MaxVMID + 1)
	if err != nil {
		return 0, err
	}
	nextId = pconf.MaxVMID
	pconf.Mutex.Unlock()
	return nextId, nil
}

func pmParallelBegin(pconf *providerConfiguration) {
	log.Print("[INFO] pmParallelBegin()")
	pconf.Mutex.Lock()
	for pconf.CurrentParallel >= pconf.MaxParallel {
		pconf.Cond.Wait()
	}
	pconf.CurrentParallel++
	pconf.Mutex.Unlock()
}

func pmParallelEnd(pconf *providerConfiguration) {
	log.Print("[INFO] pmParallelEnd()")
	pconf.Mutex.Lock()
	pconf.CurrentParallel--
	pconf.Cond.Signal()
	pconf.Mutex.Unlock()
}

func resourceId(targetNode string, resType string, vmId int) string {
	log.Print("[INFO] resourceId()")
	return fmt.Sprintf("%s/%s/%d", targetNode, resType, vmId)
}

var rxRsId = regexp.MustCompile("([^/]+)/([^/]+)/(\\d+)")

func parseResourceId(resId string) (targetNode string, resType string, vmId int, err error) {
	log.Print("[INFO] parseResourceId()")
	idMatch := rxRsId.FindStringSubmatch(resId)
	if idMatch == nil {
		err = fmt.Errorf("Invalid resource id: %s", resId)
	}
	targetNode = idMatch[1]
	resType = idMatch[2]
	vmId, err = strconv.Atoi(idMatch[3])
	return
}
