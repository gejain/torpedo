/*
PDS API

Portworx Data Services API Server

API version: 1.0.0
*/

// Code generated by OpenAPI Generator (https://openapi-generator.tech); DO NOT EDIT.

package pds

import (
	"encoding/json"
)

// ControllersServiceAccountTokenResponse struct for ControllersServiceAccountTokenResponse
type ControllersServiceAccountTokenResponse struct {
	Token *string `json:"token,omitempty"`
}

// NewControllersServiceAccountTokenResponse instantiates a new ControllersServiceAccountTokenResponse object
// This constructor will assign default values to properties that have it defined,
// and makes sure properties required by API are set, but the set of arguments
// will change when the set of required properties is changed
func NewControllersServiceAccountTokenResponse() *ControllersServiceAccountTokenResponse {
	this := ControllersServiceAccountTokenResponse{}
	return &this
}

// NewControllersServiceAccountTokenResponseWithDefaults instantiates a new ControllersServiceAccountTokenResponse object
// This constructor will only assign default values to properties that have it defined,
// but it doesn't guarantee that properties required by API are set
func NewControllersServiceAccountTokenResponseWithDefaults() *ControllersServiceAccountTokenResponse {
	this := ControllersServiceAccountTokenResponse{}
	return &this
}

// GetToken returns the Token field value if set, zero value otherwise.
func (o *ControllersServiceAccountTokenResponse) GetToken() string {
	if o == nil || o.Token == nil {
		var ret string
		return ret
	}
	return *o.Token
}

// GetTokenOk returns a tuple with the Token field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *ControllersServiceAccountTokenResponse) GetTokenOk() (*string, bool) {
	if o == nil || o.Token == nil {
		return nil, false
	}
	return o.Token, true
}

// HasToken returns a boolean if a field has been set.
func (o *ControllersServiceAccountTokenResponse) HasToken() bool {
	if o != nil && o.Token != nil {
		return true
	}

	return false
}

// SetToken gets a reference to the given string and assigns it to the Token field.
func (o *ControllersServiceAccountTokenResponse) SetToken(v string) {
	o.Token = &v
}

func (o ControllersServiceAccountTokenResponse) MarshalJSON() ([]byte, error) {
	toSerialize := map[string]interface{}{}
	if o.Token != nil {
		toSerialize["token"] = o.Token
	}
	return json.Marshal(toSerialize)
}

type NullableControllersServiceAccountTokenResponse struct {
	value *ControllersServiceAccountTokenResponse
	isSet bool
}

func (v NullableControllersServiceAccountTokenResponse) Get() *ControllersServiceAccountTokenResponse {
	return v.value
}

func (v *NullableControllersServiceAccountTokenResponse) Set(val *ControllersServiceAccountTokenResponse) {
	v.value = val
	v.isSet = true
}

func (v NullableControllersServiceAccountTokenResponse) IsSet() bool {
	return v.isSet
}

func (v *NullableControllersServiceAccountTokenResponse) Unset() {
	v.value = nil
	v.isSet = false
}

func NewNullableControllersServiceAccountTokenResponse(val *ControllersServiceAccountTokenResponse) *NullableControllersServiceAccountTokenResponse {
	return &NullableControllersServiceAccountTokenResponse{value: val, isSet: true}
}

func (v NullableControllersServiceAccountTokenResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.value)
}

func (v *NullableControllersServiceAccountTokenResponse) UnmarshalJSON(src []byte) error {
	v.isSet = true
	return json.Unmarshal(src, &v.value)
}

