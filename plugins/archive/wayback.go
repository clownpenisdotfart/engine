// Copyright © by Jeff Foley 2023. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.
// SPDX-License-Identifier: Apache-2.0

package archive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/caffix/stringset"
	"github.com/owasp-amass/engine/net/dns"
	"github.com/owasp-amass/engine/net/http"
	et "github.com/owasp-amass/engine/types"
	oam "github.com/owasp-amass/open-asset-model"
	"github.com/owasp-amass/open-asset-model/domain"
)

type wayback struct {
	URL string
}

func NewWayback() et.Plugin {
	return &wayback{
		URL: "https://web.archive.org/cdx/search/cdx?matchType=domain&fl=original&output=json&collapse=urlkey&url=",
	}
}

func (w *wayback) Start(r et.Registry) error {
	name := "Wayback-Handler"
	if err := r.RegisterHandler(&et.Handler{
		Name:       name,
		Transforms: []string{"fqdn"},
		EventType:  oam.FQDN,
		Callback:   w.check,
	}); err != nil {
		r.Log().Printf("Failed to register the %s: %v", name, err)
		return err
	}
	return nil
}

func (w *wayback) Stop() {}

func (w *wayback) check(e *et.Event) error {
	fqdn, ok := e.Asset.Asset.(*domain.FQDN)
	if !ok {
		return errors.New("failed to extract the FQDN asset")
	}

	domlt := strings.ToLower(strings.TrimSpace(fqdn.Name))
	if e.Session.Config().WhichDomain(domlt) != domlt {
		return nil
	}

	matches, err := e.Session.Config().CheckTransformations("fqdn", "fqdn", "wayback")
	if err != nil {
		return err
	}
	if !matches.IsMatch("fqdn") {
		return nil
	}

	records, err := w.query(domlt)
	if err != nil {
		return err
	}

	w.submit(e, w.process(records))
	return nil
}

func (w *wayback) query(name string) (string, error) {
	resp, err := http.RequestWebPage(context.TODO(), &http.Request{URL: w.URL + name})
	if err != nil {
		return "", fmt.Errorf("error fetching URL: %w", err)
	}

	return resp.Body, nil
}

func (w *wayback) process(records string) []string {
	var urls [][]string
	subdomains := stringset.New()

	err := json.Unmarshal([]byte(records), &urls)
	if err != nil {
		return nil
	}

	for _, url := range urls {
		if len(url) != 1 {
			continue
		}
		u := url[0]

		if n := dns.AnySubdomainRegex().FindString(u); n != "" {
			subdomains.Insert(n)
		}
	}
	return subdomains.Slice()
}

func (w *wayback) submit(e *et.Event, subs []string) {
	for _, n := range subs {
		// if the subdomain is not in scope, skip it
		if !e.Session.Config().IsDomainInScope(n) {
			continue
		}
		if a, err := e.Session.DB().Create(nil, "", &domain.FQDN{Name: n}); err == nil && a != nil {
			_ = e.Dispatcher.DispatchEvent(&et.Event{
				Name:    n,
				Asset:   a,
				Session: e.Session,
			})
		}
	}
}