// Copyright IBM Corp. 2017, 2021 All Rights Reserved.
// Licensed under the Mozilla Public License v2.0

package kms

import (
	"context"
	"fmt"
	"strings"
	"net/http"
	"time"

	"github.com/IBM-Cloud/terraform-provider-ibm/ibm/conns"
	"github.com/IBM-Cloud/terraform-provider-ibm/ibm/validate"
	kp "github.com/IBM/keyprotect-go-client"
	rc "github.com/IBM/platform-services-go-sdk/resourcecontrollerv2"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func ResourceIBMKmskeyRings() *schema.Resource {
	return &schema.Resource{
		Create:   resourceIBMKmsKeyRingCreate,
		Delete:   resourceIBMKmsKeyRingDelete,
		Read:     resourceIBMKmsKeyRingRead,
		Importer: &schema.ResourceImporter{},

		Schema: map[string]*schema.Schema{
			"instance_id": {
				Type:             schema.TypeString,
				Required:         true,
				Description:      "Key protect Instance GUID",
				ForceNew:         true,
				DiffSuppressFunc: suppressKMSInstanceIDDiff,
			},
			"key_ring_id": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Description:  "User defined unique ID for the key ring",
				ValidateFunc: validate.InvokeValidator("ibm_kms_key_rings", "key_ring_id"),
			},
			"endpoint_type": {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ValidateFunc: validate.ValidateAllowedStringValues([]string{"public", "private"}),
				Description:  "public or private",
				ForceNew:     true,
			},
		},
	}
}

func ResourceIBMKeyRingValidator() *validate.ResourceValidator {

	validateSchema := make([]validate.ValidateSchema, 0)

	validateSchema = append(validateSchema,
		validate.ValidateSchema{
			Identifier:                 "key_ring_id",
			ValidateFunctionIdentifier: validate.ValidateRegexpLen,
			Type:                       validate.TypeString,
			Required:                   true,
			Regexp:                     `^[a-zA-Z0-9-]*$`,
			MinValueLength:             2,
			MaxValueLength:             100})

	ibmKeyRingResourceValidator := validate.ResourceValidator{ResourceName: "ibm_kms_key_rings", Schema: validateSchema}
	return &ibmKeyRingResourceValidator
}

func resourceIBMKmsKeyRingCreate(d *schema.ResourceData, meta interface{}) error {
	kpAPI, err := meta.(conns.ClientSession).KeyManagementAPI()
	if err != nil {
		return err
	}

	instanceID := d.Get("instance_id").(string)
	CrnInstanceID := strings.Split(instanceID, ":")
	if len(CrnInstanceID) > 3 {
		instanceID = CrnInstanceID[len(CrnInstanceID)-3]
	}
	endpointType := d.Get("endpoint_type").(string)
	keyRingID := d.Get("key_ring_id").(string)

	rsConClient, err := meta.(conns.ClientSession).ResourceControllerV2API()
	if err != nil {
		return err
	}
	resourceInstanceGet := rc.GetResourceInstanceOptions{
		ID: &instanceID,
	}

	instanceData, resp, err := rsConClient.GetResourceInstance(&resourceInstanceGet)
	instanceCRN := instanceData.CRN
	if err != nil || instanceData == nil {
		return fmt.Errorf("[ERROR] Error retrieving resource instance: %s with resp code: %s", err, resp)
	}
	extensions := instanceData.Extensions
	URL, err := KmsEndpointURL(kpAPI, endpointType, extensions)
	if err != nil {
		return err
	}
	kpAPI.URL = URL
	kpAPI.Config.InstanceID = instanceID

	// Test fix
	tmpUrl := fmt.Sprintf("%s/api/v2/key_rings/%s", URL, keyRingID)
	tmpReq, tmpErr := http.NewRequest(http.MethodPost, tmpUrl, nil)
	if tmpErr != nil {
		return fmt.Errorf("FIX: could not create request: %s", tmpErr)
 	}

	bmxSess, tmpErr := meta.(conns.ClientSession).BluemixSession()
	if tmpErr != nil {
		return fmt.Errorf("FIX: Could not retrieve access token: %s", tmpErr)
	}
	tmpReq.Header.Set("authorization", bmxSess.Config.IAMAccessToken)
	tmpReq.Header.Set("bluemix-instance", instanceID)
	tmpClient := http.Client{
		Timeout: 30 * time.Second,
	}
	_, tmpErr2 := tmpClient.Do(tmpReq)
	if tmpErr2 != nil {
		return fmt.Errorf("FIX: could not execute request (%s): %s", tmpUrl, tmpErr2)
 	}
	// Test fix

	err = kpAPI.CreateKeyRing(context.Background(), keyRingID)
	if err != nil {
		return fmt.Errorf("[ERROR] Error while creating key ring (note: URL=%s, InstanceID=%s, KeyRingId=%s): %s", URL, instanceID, keyRingID, err)
	}
	var keyRing string
	keyRings, err2 := kpAPI.GetKeyRings(context.Background())
	if err2 != nil {
		return fmt.Errorf("[ERROR] Error while fetching key ring : %s", err2)
	}
	for _, v := range keyRings.KeyRings {
		if v.ID == keyRingID {
			keyRing = v.ID
			break
		}
	}

	d.SetId(fmt.Sprintf("%s:keyRing:%s", keyRing, *instanceCRN))

	return resourceIBMKmsKeyRingRead(d, meta)
}

func resourceIBMKmsKeyRingRead(d *schema.ResourceData, meta interface{}) error {
	kpAPI, err := meta.(conns.ClientSession).KeyManagementAPI()
	if err != nil {
		return err
	}
	id := strings.Split(d.Id(), ":keyRing:")
	if len(id) < 2 {
		return fmt.Errorf("[ERROR] Incorrect ID %s: Id should be a combination of keyRingID:keyRing:InstanceCRN", d.Id())
	}
	crn := id[1]
	crnData := strings.Split(crn, ":")
	endpointType := d.Get("endpoint_type").(string)
	instanceID := crnData[len(crnData)-3]

	rsConClient, err := meta.(conns.ClientSession).ResourceControllerV2API()
	if err != nil {
		return err
	}
	resourceInstanceGet := rc.GetResourceInstanceOptions{
		ID: &instanceID,
	}

	instanceData, resp, err := rsConClient.GetResourceInstance(&resourceInstanceGet)
	if err != nil || instanceData == nil {
		return fmt.Errorf("[ERROR] Error retrieving resource instance: %s with resp code: %s", err, resp)
	}
	extensions := instanceData.Extensions
	URL, err := KmsEndpointURL(kpAPI, endpointType, extensions)
	if err != nil {
		return err
	}
	kpAPI.URL = URL
	kpAPI.Config.InstanceID = instanceID
	_, err = kpAPI.GetKeyRings(context.Background())
	if err != nil {
		kpError := err.(*kp.Error)
		if kpError.StatusCode == 404 || kpError.StatusCode == 409 {
			d.SetId("")
			return nil
		}
		return fmt.Errorf("[ERROR] Get Key Rings failed with error: %s", err)
	}

	d.Set("instance_id", instanceID)
	if strings.Contains((kpAPI.URL).String(), "private") || strings.Contains(kpAPI.Config.BaseURL, "private") {
		d.Set("endpoint_type", "private")
	} else {
		d.Set("endpoint_type", "public")
	}
	d.Set("key_ring_id", id[0])
	return nil
}

func resourceIBMKmsKeyRingDelete(d *schema.ResourceData, meta interface{}) error {
	kpAPI, err := meta.(conns.ClientSession).KeyManagementAPI()
	if err != nil {
		return err
	}
	id := strings.Split(d.Id(), ":keyRing:")
	crn := id[1]
	crnData := strings.Split(crn, ":")
	endpointType := d.Get("endpoint_type").(string)
	instanceID := crnData[len(crnData)-3]

	rsConClient, err := meta.(conns.ClientSession).ResourceControllerV2API()
	if err != nil {
		return err
	}
	resourceInstanceGet := rc.GetResourceInstanceOptions{
		ID: &instanceID,
	}

	instanceData, resp, err := rsConClient.GetResourceInstance(&resourceInstanceGet)
	if err != nil || instanceData == nil {
		return fmt.Errorf("[ERROR] Error retrieving resource instance: %s with resp code: %s", err, resp)
	}
	extensions := instanceData.Extensions
	URL, err := KmsEndpointURL(kpAPI, endpointType, extensions)
	if err != nil {
		return err
	}
	kpAPI.URL = URL
	kpAPI.Config.InstanceID = instanceID
	err1 := kpAPI.DeleteKeyRing(context.Background(), id[0])
	if err1 != nil {
		kpError := err1.(*kp.Error)
		if kpError.StatusCode == 404 || kpError.StatusCode == 409 {
			return nil
		} else {
			return fmt.Errorf(" failed to Destroy key ring with error: %s", err1)
		}
	}
	return nil

}
