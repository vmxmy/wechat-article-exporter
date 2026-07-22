package mcp

func emptySchema() map[string]any {
	return objectSchema(nil)
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func integerProperty(minimum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum}
}

func stringArrayProperty(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description}
}

func accountQuerySchema() map[string]any {
	return objectSchema(map[string]any{
		"keyword": stringProperty("Account name or keyword"),
		"offset":  integerProperty(0),
		"limit":   integerProperty(1),
	})
}

func articleQuerySchema() map[string]any {
	return objectSchema(map[string]any{
		"accountId":   stringProperty("Local account identifier"),
		"albumId":     stringProperty("Local album identifier"),
		"keyword":     stringProperty("Title, digest, or text keyword"),
		"author":      stringProperty("Article author"),
		"state":       stringProperty("Normalized article state"),
		"hasContent":  map[string]any{"type": "boolean"},
		"hasComments": map[string]any{"type": "boolean"},
		"sort":        stringProperty("Stable sort expression"),
		"offset":      integerProperty(0),
		"limit":       integerProperty(1),
	})
}

func albumQuerySchema() map[string]any {
	return objectSchema(map[string]any{
		"accountId": stringProperty("Local account identifier"),
		"keyword":   stringProperty("Album name keyword"),
		"offset":    integerProperty(0),
		"limit":     integerProperty(1),
	})
}

func syncSchema() map[string]any {
	return objectSchema(map[string]any{
		"accountId":   stringProperty("Account to synchronize"),
		"incremental": map[string]any{"type": "boolean"},
		"range":       map[string]any{"type": "string", "enum": []string{"24h", "1d", "3d", "7d", "1m", "3m", "6m", "1y", "all", "point"}},
		"pageSize":    integerProperty(1),
	}, "accountId")
}

func albumSyncSchema() map[string]any {
	return objectSchema(map[string]any{
		"accountId": stringProperty("Account owning the album"),
		"albumId":   stringProperty("Album to synchronize"),
	}, "accountId", "albumId")
}

func downloadSchema() map[string]any {
	return objectSchema(map[string]any{
		"kind":       map[string]any{"type": "string", "enum": []string{"article", "resources", "metadata", "comments", "paid"}},
		"articleIds": stringArrayProperty("Local article identifiers"),
		"urls":       stringArrayProperty("WeChat article URLs"),
		"force":      map[string]any{"type": "boolean"},
	})
}

func exportSchema() map[string]any {
	return objectSchema(map[string]any{
		"articleIds": stringArrayProperty("Local article identifiers"),
		"format":     map[string]any{"type": "string", "enum": []string{"html", "markdown", "text", "json", "excel", "docx", "pdf"}},
		"outputRoot": stringProperty("Local output directory"),
		"selection":  map[string]any{"type": "object"},
		"options":    map[string]any{"type": "object"},
	}, "format")
}

func jobQuerySchema() map[string]any {
	return objectSchema(map[string]any{
		"kind":   stringProperty("Job kind"),
		"states": stringArrayProperty("Job states"),
		"offset": integerProperty(0),
		"limit":  integerProperty(1),
	})
}

func contentSchema() map[string]any {
	return objectSchema(map[string]any{
		"articleId": stringProperty("Local article identifier"),
		"kind":      map[string]any{"type": "string", "enum": []string{"html", "markdown", "text", "json"}},
	}, "articleId", "kind")
}

func confirmationSchema(properties map[string]any, required ...string) map[string]any {
	properties[destructiveArgument] = stringProperty("Exact destructive confirmation value")
	required = append(required, destructiveArgument)
	return objectSchema(properties, required...)
}

func sensitiveSchema() map[string]any {
	return objectSchema(map[string]any{
		"operation":        stringProperty("Sensitive local operation name"),
		"confirmSensitive": stringProperty("Exact sensitive-operation confirmation"),
		"payload":          map[string]any{"type": "object"},
	}, "operation", "confirmSensitive")
}

func pageSchema(item string) map[string]any {
	return objectSchema(map[string]any{
		"items":  map[string]any{"type": "array", "items": map[string]any{"type": "object", "title": item}},
		"total":  integerProperty(0),
		"offset": integerProperty(0),
		"limit":  integerProperty(0),
	}, "items", "total", "offset", "limit")
}

func objectOutput(title string) map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"title":   title,
	}
}

func jobOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"jobId":   stringProperty("Persistent job identifier"),
		"state":   stringProperty("Current job state"),
		"kind":    stringProperty("Job kind"),
		"profile": stringProperty("Profile that owns the job"),
	}, "jobId", "state", "kind")
}
