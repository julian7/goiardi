/* Environments. */

/*
 * Copyright (c) 2013-2014, Jeremy Bingham (<jbingham@gmail.com>)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package environment provides... environments. They're like roles, but more
// so, except without run lists. They're a convenient way to share many
// attributes and cookbook version constraints among many servers.
package environment

import (
	"github.com/ctdk/goiardi/data_store"
	"github.com/ctdk/goiardi/cookbook"
	"github.com/ctdk/goiardi/util"
	"github.com/ctdk/goiardi/indexer"
	"fmt"
	"sort"
	"net/http"
)

type ChefEnvironment struct {
	Name string `json:"name"`
	ChefType string `json:"chef_type"`
	JsonClass string `json:"json_class"`
	Description string `json:"description"`
	Default map[string]interface{} `json:"default_attributes"`
	Override map[string]interface{} `json:"override_attributes"`
	CookbookVersions map[string]string `json:"cookbook_versions"`
}

func New(name string) (*ChefEnvironment, util.Gerror){
	ds := data_store.New()
	if _, found := ds.Get("env", name); found || name == "_default" {
		err := util.Errorf("Environment already exists")
		return nil, err
	}
	if !util.ValidateEnvName(name){
		err := util.Errorf("Field 'name' invalid")
		err.SetStatus(http.StatusBadRequest)
		return nil, err
	}
	env := &ChefEnvironment{
		Name: name,
		ChefType: "environment",
		JsonClass: "Chef::Environment",
		Default: map[string]interface{}{},
		Override: map[string]interface{}{},
		CookbookVersions: map[string]string{},
	}
	return env, nil
}

// Create a new environment from JSON uploaded to the server.
func NewFromJson(json_env map[string]interface{}) (*ChefEnvironment, util.Gerror){
	env, err := New(json_env["name"].(string))
	if err != nil {
		return nil, err
	}
	err = env.UpdateFromJson(json_env)
	if err != nil {
		return nil, err
	}
	return env, nil
}

// Updates an existing environment from JSON uploaded to the server.
func (e *ChefEnvironment)UpdateFromJson(json_env map[string]interface{}) util.Gerror {
	if e.Name != json_env["name"].(string) {
		err := util.Errorf("Environment name %s and %s from JSON do not match", e.Name, json_env["name"].(string))
		return err
	} else if e.Name == "_default" {
		err := util.Errorf("Default environment cannot be modified.")
		return err
	}

	/* Validations */
	valid_elements := []string{ "name", "chef_type", "json_class", "description", "default_attributes", "override_attributes", "cookbook_versions" }
	ValidElem:
	for k := range json_env {
		for _, i := range valid_elements {
			if k == i {
				continue ValidElem
			}
		}
		err := util.Errorf("Invalid key %s in request body", k)
		return err
	}

	var verr util.Gerror

	attrs := []string{ "default_attributes", "override_attributes" }
	for _, a := range attrs {
		json_env[a], verr = util.ValidateAttributes(a, json_env[a])
		if verr != nil {
			return verr
		}
	}

	json_env["json_class"], verr = util.ValidateAsFieldString(json_env["json_class"])
	if verr != nil {
		if verr.Error() == "Field 'name' nil" {
			json_env["json_class"] = e.JsonClass
		} else {
			return verr
		}
	} else {
		if json_env["json_class"].(string) != "Chef::Environment" {
			verr = util.Errorf("Field 'json_class' invalid")
			return verr
		}
	}


	json_env["chef_type"], verr = util.ValidateAsFieldString(json_env["chef_type"])
	if verr != nil {
		if verr.Error() == "Field 'name' nil" {
			json_env["chef_type"] = e.ChefType
		} else {
			return verr
		}
	} else {
		if json_env["chef_type"].(string) != "environment" {
			verr = util.Errorf("Field 'chef_type' invalid")
			return verr
		}
	}

	json_env["cookbook_versions"], verr = util.ValidateAttributes("cookbook_versions", json_env["cookbook_versions"])
	if verr != nil {
		return verr
	} else {
		for k, v := range json_env["cookbook_versions"].(map[string]interface{}) {
			if !util.ValidateEnvName(k) || k == "" {
				merr := util.Errorf("Cookbook name %s invalid", k)
				merr.SetStatus(http.StatusBadRequest)
				return merr
			}

			if v == nil {
				verr = util.Errorf("Invalid version number")
				return verr
			}
			_, verr = util.ValidateAsConstraint(v)
			if verr != nil {
				/* try validating as a version */
				v, verr = util.ValidateAsVersion(v)
				if verr != nil {
					return verr
				}
			}
		}
	}

	json_env["description"], verr = util.ValidateAsString(json_env["description"])
	if verr != nil {
		if verr.Error() == "Field 'name' missing" {
			json_env["description"] = ""
		} else {
			return verr
		}
	}

	e.ChefType = json_env["chef_type"].(string)
	e.JsonClass = json_env["json_class"].(string)
	e.Description = json_env["description"].(string)
	e.Default = json_env["default_attributes"].(map[string]interface{})
	e.Override = json_env["override_attributes"].(map[string]interface{})
	/* clear out, then loop over the cookbook versions */
	e.CookbookVersions = make(map[string]string, len(json_env["cookbook_versions"].(map[string]interface{})))
	for c, v := range json_env["cookbook_versions"].(map[string]interface{}){
		e.CookbookVersions[c] = v.(string)
	}

	return nil
}

func Get(env_name string) (*ChefEnvironment, error){
	if env_name == "_default" {
		return defaultEnvironment(), nil
	}
	ds := data_store.New()
	env, found := ds.Get("env", env_name)
	if !found {
		err := fmt.Errorf("Cannot load environment %s", env_name)
		return nil, err
	}
	return env.(*ChefEnvironment), nil
}

// Creates the default environment on startup
func MakeDefaultEnvironment() {
	ds := data_store.New()
	// only create the new default environment if we don't already have one
	// saved
	if _, found := ds.Get("env", "_default"); found {
		return
	}
	de := defaultEnvironment()
	ds.Set("env", de.Name, de)
	indexer.IndexObj(de)
}

func defaultEnvironment() (*ChefEnvironment) {
	return &ChefEnvironment{
		Name: "_default",
		ChefType: "environment",
		JsonClass: "Chef::Environment",
		Description: "The default Chef environment",
		Default: map[string]interface{}{},
		Override: map[string]interface{}{},
		CookbookVersions: map[string]string{},
	}
}

func (e *ChefEnvironment) Save() error {
	if e.Name == "_default" {
		err := fmt.Errorf("The '_default' environment cannot be modified.")
		return err
	}
	ds := data_store.New()
	ds.Set("env", e.Name, e)
	indexer.IndexObj(e)
	return nil
}

func (e *ChefEnvironment) Delete() error {
	if e.Name == "_default" {
		err := fmt.Errorf("The '_default' environment cannot be modified.")
		return err
	}
	ds := data_store.New()
	indexer.DeleteItemFromCollection("environment", e.Name)
	ds.Delete("env", e.Name)
	return nil
}

// Get a list of all environments on this server.
func GetList() []string {
	ds := data_store.New()
	env_list := ds.GetList("env")
	env_list = append(env_list, "_default")
	return env_list
}

func (e *ChefEnvironment) GetName() string {
	return e.Name
}

func (e *ChefEnvironment) URLType() string {
	return "environments"
}

func (e *ChefEnvironment) cookbookList() []*cookbook.Cookbook {
	cb_list := cookbook.GetList()
	cookbooks := make([]*cookbook.Cookbook, len(cb_list))
	for i, cb := range cb_list {
		cookbooks[i], _ = cookbook.Get(cb)
	}
	return cookbooks
}

// Gets a list of the cookbooks and their versions available to this 
// environment.
func (e *ChefEnvironment) AllCookbookHash(num_versions interface{}) map[string]interface{} {
	cb_hash := make(map[string]interface{})
	cb_list := e.cookbookList()
	for _, cb := range cb_list {
		if cb == nil {
			continue
		}
		cb_hash[cb.Name] = cb.ConstrainedInfoHash(num_versions, e.CookbookVersions[cb.Name])
	}
	return cb_hash
}

// Gets a list of recipes available to this environment.
func (e *ChefEnvironment) RecipeList() []string {
	recipe_list := make(map[string]string)
	cb_list := e.cookbookList()
	for _, cb := range cb_list {
		if cb == nil {
			continue
		}
		cbv := cb.LatestConstrained(e.CookbookVersions[cb.Name])
		if cbv == nil {
			continue
		}
		rlist, _ := cbv.RecipeList()
		
		for _, recipe := range rlist {
			recipe_list[recipe] = recipe
		}
	}
	sorted_recipes := make([]string, len(recipe_list))
	i := 0
	for k := range recipe_list {
		sorted_recipes[i] = k
		i++
	}
	sort.Strings(sorted_recipes)
	return sorted_recipes
}

/* Search indexing methods */

func (e *ChefEnvironment) DocId() string {
	return e.Name
}

func (e *ChefEnvironment) Index() string {
	return "environment"
}

func (e *ChefEnvironment) Flatten() []string {
	flatten := util.FlattenObj(e)
	indexified := util.Indexify(flatten)
	return indexified
}
