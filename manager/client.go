package manager

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/indihub-space/agent/lib"
)

const (
	False = "False"
	True  = "True"
)

type client struct {
	httpClient http.Client
	addr       string
}

func NewClient(managerServerAddr string) *client {
	return &client{
		httpClient: http.Client{},
		addr:       managerServerAddr,
	}
}

func (c *client) GetStatus() (bool, string, error) {
	resp, err := c.httpClient.Get(fmt.Sprintf("http://%s/api/server/status", c.addr))
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	respJson, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, "", err
	}

	respData := []map[string]string{}
	if err := json.Unmarshal(respJson, &respData); err != nil {
		return false, "", err
	}

	if len(respData) != 1 {
		return false, "", fmt.Errorf("wrong slice length in INDI-server manager reply %v", respData)
	}

	return respData[0]["status"] == True, respData[0]["active_profile"], nil
}

func (c *client) StopServer() error {
	resp, err := c.httpClient.Post(fmt.Sprintf("http://%s/api/server/stop", c.addr),
		"application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("could not stop INDI-server, response code %d", resp.StatusCode)
	}

	return nil
}

func (c *client) GetProfile(profile string) (*lib.INDIProfile, error) {
	resp, err := c.httpClient.Get(fmt.Sprintf("http://%s/api/profiles/%s", c.addr, profile))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respJson, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	indiProfile := &lib.INDIProfile{}
	if err := json.Unmarshal(respJson, indiProfile); err != nil {
		return nil, err
	}

	return indiProfile, nil
}

func (c *client) StartProfile(profile string) error {
	resp, err := c.httpClient.Post(fmt.Sprintf("http://%s/api/server/start/%s", c.addr, profile),
		"application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("could not start INDI-server with profile %s, response code %d",
			profile,
			resp.StatusCode,
		)
	}

	return nil
}

func (c *client) GetDrivers() ([]*lib.INDIDriver, error) {
	resp, err := c.httpClient.Get(fmt.Sprintf("http://%s/api/server/drivers", c.addr))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respJson, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	drivers := []*lib.INDIDriver{}
	if err := json.Unmarshal(respJson, &drivers); err != nil {
		return nil, err
	}

	return drivers, nil
}
