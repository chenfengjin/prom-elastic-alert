package boot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/dream-mo/prom-elastic-alert/conf"
	"github.com/dream-mo/prom-elastic-alert/utils"
	"github.com/dream-mo/prom-elastic-alert/utils/xelastic"
)

type AlertState int

const (
	Pending AlertState = iota
	Resolved
)

type AlertContent struct {
	Rule     *conf.Rule
	Match    *Match
	StartsAt *time.Time
	EndsAt   *time.Time
	State    AlertState
}

type AlertMessage struct {
	UniqueId string `json:"id"`
	Path     string `json:"path"`
	Payload  string `json:"payload"`
	StartsAt *time.Time
}

type AlertSampleMessage struct {
	ES    conf.EsConfig `json:"es"`
	Index string        `json:"index"`
	Ids   []string      `json:"ids"`
}

func (ac *AlertContent) HasResolved() bool {
	return ac.State == Resolved
}

func (ac *AlertContent) GetAlertMessage(generatorURL string, msg AlertSampleMessage) string {
	body := conf.BuildFindByIdsDSLBody(msg.Ids)

	client := xelastic.NewElasticClient(msg.ES, msg.ES.Version)
	hits, _, _ := client.FindByDSL(msg.Index, body, nil)
	var errorMsg, appName, env string

	sourceI := hits[0].(map[string]any)["_source"]
	if sourceI != nil {
		source := sourceI.(map[string]any)
		if source["@message"] != nil {
			errorMsg = source["@message"].(string)
		}
		if source["@appname"] != nil {
			appName = source["@appname"].(string)
		}
		if source["@env"] != nil {
			env = source["@env"].(string)
		}
	}

	extra := hits[0].(map[string]any)

	//es_id := (hits[0].(map[string]any)["_id"]).(string)
	uniqueId := ac.Rule.UniqueId
	payload := ac.getHttpPayload(generatorURL, errorMsg, appName, env, extra)
	path := ac.Rule.FilePath
	message := AlertMessage{
		UniqueId: uniqueId,
		Path:     path,
		Payload:  payload,
		StartsAt: ac.StartsAt,
	}
	b, _ := json.Marshal(message)
	return string(b)
}

func (ac *AlertContent) getUrlHashKey() string {
	return utils.MD5(strings.Join(ac.Match.Ids, ""))
}

func (ac *AlertContent) getHttpPayload(generatorURL string, errorMsg, appName, env string, extra map[string]any) string {
	end := ac.EndsAt
	ends := ""
	if end != nil {
		ends = end.UTC().Format(time.RFC3339)
	}
	data := ac.mapCopy(ac.Rule.Query.Labels)
	data["value"] = fmt.Sprintf("%d", ac.Match.HitsNumber)
	data["generatorURL"] = generatorURL
	data["errorMsg"] = errorMsg
	data["appname"] = appName
	data["env"] = env
	for k, v := range extra {
		if value, ok := v.(string); ok {
			data[k] = value
		}
	}
	annotations := ac.mapCopy(ac.Rule.Query.Annotations)
	ac.parseTemplate(annotations, data)
	b := map[string]any{
		"labels":       ac.Rule.Query.Labels,
		"annotations":  annotations,
		"startsAt":     ac.StartsAt.UTC().Format(time.RFC3339),
		"generatorURL": generatorURL,
	}
	//fmt.Println(generatorURL)
	if ends != "" {
		b["endsAt"] = ends
	}
	body := []map[string]any{
		b,
	}
	payload, _ := json.Marshal(body)
	return string(payload)
}

func (ac *AlertContent) parseTemplate(m map[string]string, data any) {
	for k, tpl := range m {
		t := template.New(k)
		parse, err := t.Parse(tpl)
		if err != nil {
			continue
		}
		bf := bytes.NewBufferString("")
		err = parse.Execute(bf, data)
		if err != nil {
			continue
		} else {
			m[k] = bf.String()
		}
	}
}

func (ac *AlertContent) mapCopy(m map[string]string) map[string]string {
	data := map[string]string{}
	for k, v := range m {
		data[k] = v
	}
	return data
}
