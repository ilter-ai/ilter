// Package smartrouter combines routing decision (which model) with provider
// dispatch (which provider) into one unified concern. It selects the most
// cost-effective model via complexity scoring and routing rules, then selects
// a provider for that model using round-robin/cheapest/first-available strategy.
//
// Created by merging internal/smartrouter and internal/smartrouter.
package smartrouter
