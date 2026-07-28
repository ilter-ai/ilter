// Package modelregistry is an in-memory cache of model metadata (ModelInfo),
// populated at startup from the provider_models DB table. It provides read
// access to model properties: tier ("economy" / "standard" / "premium"),
// costs, capabilities (tool support), context limits, and provider mappings.
//
// Relationship to loadbalancer and smartrouter:
//
//	modelregistry is a pure data layer — it stores what models exist and
//	their properties. loadbalancer handles provider dispatch for a given
//	model. smartrouter handles routing decisions (which model to use).
//	Both depend on modelregistry for model metadata, but modelregistry has
//	no dependency on either.
//
// The package is intentionally minimal: it is a shared cache with no business
// logic beyond helper methods (SupportsTools, CanonicalModelID).
package catalog
