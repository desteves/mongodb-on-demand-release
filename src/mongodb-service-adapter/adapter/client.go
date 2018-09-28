package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/cf-platform-eng/mongodb-on-demand-release/src/mongodb-service-adapter/digest"
	"github.com/tidwall/gjson"
)

type OMClient struct {
	Url      string
	Username string
	ApiKey   string
}

type Automation struct {
	MongoDbVersions []MongoDbVersionsType
}

type MongoDbVersionsType struct {
	Name string
}

type Group struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	AgentAPIKey string         `json:"agentApiKey"`
	HostCounts  map[string]int `json:"hostCounts"`
}

type GroupCreateRequest struct {
	Name  string   `json:"name"`
	OrgId string   `json:"orgId,omitempty"`
	Tags  []string `json:"tags"`
}

type GroupUpdateRequest struct {
	Tags []string `json:"tags"`
}

type GroupHosts struct {
	TotalCount int `json:"totalCount"`
}

type DocContext struct {
	ID                   string
	Key                  string
	AdminPassword        string
	Version              string
	CompatibilityVersion string
	Nodes                []string
	Cluster              *Cluster
	Password             string
	RequireSSL           bool
}

type Cluster struct {
	Routers       []string
	ConfigServers []string
	Shards        [][]string
}

func (oc *OMClient) LoadDoc(p string, ctx *DocContext) (string, error) {
	t, ok := plans[p]
	if !ok {
		return "", fmt.Errorf("plan %q not found", p)
	}

	if ctx.Password == "" {
		var err error
		ctx.Password, err = GenerateString(32)
		if err != nil {
			panic(err)
		}
	}

	if strings.HasPrefix(ctx.Version, "3.4") {
		ctx.CompatibilityVersion = "3.4"
	} else if strings.HasPrefix(ctx.Version, "3.6") {
		ctx.CompatibilityVersion = "3.6"
	} else if strings.HasPrefix(ctx.Version, "4.0") {
		ctx.CompatibilityVersion = "4.0"
	}

	b := bytes.Buffer{}
	if err := t.Execute(&b, ctx); err != nil {
		return "", err
	}
	return b.String(), nil
}

//GetGroupByName returns group if found.
func (oc *OMClient) GetGroupByName(name string) (Group, error) {
	var group Group
	b, err := oc.doRequest("GET", "/api/public/v1.0/groups/byName/"+name)
	if err != nil {
		return group, err
	}
	if err = json.Unmarshal(b, &group); err != nil {
		return group, err
	}
	return group, nil
}

//CreateGroup returns existing if found, else creates a new one.
func (oc *OMClient) CreateGroup(id string, request GroupCreateRequest) (Group, error) {
	var group Group

	if request.Name == "" {
		request.Name = fmt.Sprintf("PCF_%s", id)
	}
	req, err := json.Marshal(request)
	if err != nil {
		return group, err
	}

	group, err = GetGroupByName(request.Name)
	if err != nil {
		return group, err
	}
	if group.Name == request.Name {
		return group, nil
	}
	b, err = oc.doRequest("POST", "/api/public/v1.0/groups", bytes.NewReader(req))
	if err != nil {
		return group, err
	}

	if err = json.Unmarshal(b, &group); err != nil {
		return group, err
	}
	return group, nil
}

func (oc *OMClient) UpdateGroup(id string, request GroupUpdateRequest) (Group, error) {
	var group Group

	req, err := json.Marshal(request)
	if err != nil {
		return group, err
	}
	b, err := oc.doRequest("PATCH", fmt.Sprintf("/api/public/v1.0/groups/%s", id), bytes.NewReader(req))
	if err != nil {
		return group, err
	}

	if err = json.Unmarshal(b, &group); err != nil {
		return group, err
	}
	return group, nil
}

func (oc *OMClient) GetGroup(groupID string) (Group, error) {
	var group Group

	b, err := oc.doRequest("GET", fmt.Sprintf("/api/public/v1.0/groups/%s", groupID), nil)
	if err != nil {
		return group, err
	}

	if err = json.Unmarshal(b, &group); err != nil {
		return group, err
	}
	return group, nil
}

func (oc *OMClient) DeleteGroup(groupID string) error {
	_, err := oc.doRequest("DELETE", fmt.Sprintf("/api/public/v1.0/groups/%s", groupID), nil)
	return err
}

func (oc *OMClient) GetGroupHosts(groupID string) (GroupHosts, error) {
	var groupHosts GroupHosts

	b, err := oc.doRequest("GET", fmt.Sprintf("/api/public/v1.0/groups/%s/hosts", groupID), nil)
	if err != nil {
		return groupHosts, err
	}

	if err = json.Unmarshal(b, &groupHosts); err != nil {
		return groupHosts, err
	}
	return groupHosts, nil
}

func (oc *OMClient) GetGroupHostnames(groupID string, planID string) ([]string, error) {
	b, err := oc.doRequest("GET", fmt.Sprintf("/api/public/v1.0/groups/%s/hosts", groupID), nil)
	if err != nil {
		return nil, err
	}

	groupHostnames := gjson.GetBytes(b, fmt.Sprintf(`results.#.hostname`))
	if planID == "sharded_cluster" {
		groupHostnames = gjson.GetBytes(b, fmt.Sprintf(`results.#[typeName="SHARD_MONGOS"]#.hostname`))
	}

	servers := make([]string, len(groupHostnames.Array()))
	for i, node := range groupHostnames.Array() {
		servers[i] = fmt.Sprintf("%s:28000", node)
	}

	return servers, nil
}

func (oc *OMClient) ConfigureGroup(configurationDoc string, groupID string) error {
	u := fmt.Sprintf("/api/public/v1.0/groups/%s/automationConfig", groupID)
	b, err := oc.doRequest("PUT", u, strings.NewReader(configurationDoc))
	if err != nil {
		return err
	}
	log.Println(string(b))

	return nil
}

func (oc *OMClient) ConfigureMonitoringAgent(configurationDoc string, groupID string) error {
	u := fmt.Sprintf("/api/public/v1.0/groups/%s/automationConfig/monitoringAgentConfig", groupID)
	b, err := oc.doRequest("PUT", u, strings.NewReader(configurationDoc))
	if err != nil {
		return err
	}
	log.Println(string(b))

	return nil
}

func (oc *OMClient) ConfigureBackupAgent(configurationDoc string, groupID string) error {
	u := fmt.Sprintf("/api/public/v1.0/groups/%s/automationConfig/backupAgentConfig", groupID)
	b, err := oc.doRequest("PUT", u, strings.NewReader(configurationDoc))
	if err != nil {
		return err
	}
	log.Println(string(b))

	return nil
}
func (oc *OMClient) GetAvailableVersions(groupID string) (Automation, error) {
	var versions Automation

	b, err := oc.doRequest("GET", fmt.Sprintf("/api/public/v1.0/groups/%s/automationConfig", groupID), nil)
	if err != nil {
		return versions, err
	}

	if err = json.Unmarshal(b, &versions); err != nil {
		return versions, err
	}
	return versions, nil
}

func (oc *OMClient) GetLatestVersion(groupID string) string {
	cfg, err := oc.GetAvailableVersions(groupID)
	if err != nil {
		panic(err)
	}

	versions := make([]string, len(cfg.MongoDbVersions))
	n := 0
	for _, i := range cfg.MongoDbVersions {
		if !strings.HasSuffix(i.Name, "ent") {
			versions[n] = i.Name
			n++
		}
	}
	versions = versions[:n]
	latestVersion := versions[len(versions)-1]

	return latestVersion
}

func (oc *OMClient) ValidateVersion(groupID string, version string) (string, error) {
	b, err := oc.doRequest("GET", fmt.Sprintf("/api/public/v1.0/groups/%s/automationConfig", groupID), nil)
	if err != nil {
		return "", err
	}

	v := gjson.GetBytes(b, fmt.Sprintf(`mongoDbVersions.#[name="%s"].name`, version))
	log.Printf("Using %q version of MongoDB", v.String())
	if v.String() == "" {
		log.Fatalf("failed to find expected version, got %s", version)
	}

	return v.String(), nil
}

func (oc *OMClient) doRequest(method string, path string, body io.Reader) ([]byte, error) {
	uri := fmt.Sprintf("%s%s", oc.Url, path)
	req, err := http.NewRequest(method, uri, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if err = digest.ApplyDigestAuth(oc.Username, oc.ApiKey, uri, req); err != nil {
		return nil, err
	}

	dump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		return nil, err
	}
	log.Printf("API Request: %q", dump)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("%s %s error: %v", method, uri, err)
		return nil, err
	}
	defer res.Body.Close()

	dump, err = httputil.DumpResponse(res, true)
	if err != nil {
		return nil, err
	}
	log.Printf("API Response: %q", dump)

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s request error: code=%d body=%q", method, path, res.StatusCode, b)
	}
	return b, nil
}
