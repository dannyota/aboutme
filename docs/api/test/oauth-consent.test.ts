import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8")) as any;

const OPERATIONS = {
  getOAuthConsent: { method: "get", path: "/oauth/consent" },
  postOAuthConsentDecision: { method: "post", path: "/oauth/consent" },
  listAgentGrants: { method: "get", path: "/me/agents" },
  revokeAgentGrant: { method: "delete", path: "/me/agents/{grantId}" },
} as const;

function operation(operationId: keyof typeof OPERATIONS): any {
  const { method, path } = OPERATIONS[operationId];
  return doc.paths[path]?.[method];
}

function schema(ref: string): any {
  const name = ref.replace("#/components/schemas/", "");
  return doc.components.schemas[name];
}

function response(refOrResponse: any): any {
  if (!refOrResponse.$ref) return refOrResponse;
  const name = refOrResponse.$ref.replace("#/components/responses/", "");
  return doc.components.responses[name];
}

function responseDataSchema(response: any): any {
  const responseSchema = response.content["application/json"].schema;
  if (responseSchema.$ref) {
    const resolved = schema(responseSchema.$ref);
    const data = resolved.properties?.data;
    return data?.$ref ? schema(data.$ref) : (data ?? resolved);
  }
  const data = responseSchema.allOf.find((part: any) => part.properties?.data);
  const dataSchema = data.properties.data;
  return dataSchema.$ref ? schema(dataSchema.$ref) : dataSchema;
}

describe("OAuth consent and agent grant OpenAPI contract", () => {
  it("defines exactly the four session-authenticated OAuthAgentAccess operations", () => {
    const tagged: string[] = [];
    for (const [path, item] of Object.entries<any>(doc.paths)) {
      for (const [method, op] of Object.entries<any>(item)) {
        if (method === "parameters" || method === "servers") continue;
        if (op?.tags?.includes("OAuthAgentAccess")) {
          tagged.push(`${method.toUpperCase()} ${path}`);
        }
      }
    }

    expect(tagged).toHaveLength(4);
    for (const [operationId, { method, path }] of Object.entries(OPERATIONS)) {
      const op = operation(operationId as keyof typeof OPERATIONS);
      expect(op?.operationId, `${method.toUpperCase()} ${path}`).toBe(
        operationId,
      );
      expect(op?.tags, `${method.toUpperCase()} ${path}`).toEqual([
        "OAuthAgentAccess",
      ]);
      expect(op?.security, `${method.toUpperCase()} ${path}`).toEqual(
        method === "get"
          ? [{ sessionCookie: [] }]
          : [{ sessionCookie: [], csrfToken: [] }],
      );
    }
  });

  it("keeps raw OAuth protocol and MCP paths out of the REST contract", () => {
    expect(Object.keys(doc.paths)).not.toContain("/mcp");
    expect(Object.keys(doc.paths)).not.toContain("/oauth/authorize");
    expect(Object.keys(doc.paths)).not.toContain("/oauth/token");
    expect(Object.keys(doc.paths)).not.toContain("/oauth/register");
    expect(Object.keys(doc.paths)).not.toContain("/oauth/revoke");
    expect(
      doc.tags.find((tag: any) => tag.name === "OAuthAgentAccess").description,
    ).toMatch(/raw OAuth protocol endpoints.*MCP|\/mcp.*not.*OpenAPI/is);
  });

  it("uses the complete, closed authorize query and scope enum list", () => {
    const consent = operation("getOAuthConsent");
    const parameters = Object.fromEntries(
      consent.parameters.map((parameter: any) => [parameter.name, parameter]),
    );
    expect(Object.keys(parameters).sort()).toEqual([
      "client_id",
      "code_challenge",
      "code_challenge_method",
      "redirect_uri",
      "response_type",
      "scope",
      "state",
    ]);
    expect(parameters.client_id.required).toBe(true);
    expect(parameters.redirect_uri.required).toBe(true);
    expect(parameters.response_type.schema).toEqual({
      type: "string",
      const: "code",
    });
    expect(parameters.scope.schema).toEqual({
      type: "string",
      enum: ["resumes:read", "resumes:write", "resumes:read resumes:write"],
    });
    expect(parameters.code_challenge_method.schema).toEqual({
      type: "string",
      const: "S256",
    });
  });

  it("closes request and success response objects and does not expose secrets", () => {
    const decision = operation("postOAuthConsentDecision");
    const decisionSchema = schema(
      decision.requestBody.content["application/json"].schema.$ref,
    );
    expect(decisionSchema.additionalProperties).toBe(false);
    expect(decisionSchema.required).toEqual([
      "client_id",
      "redirect_uri",
      "response_type",
      "scope",
      "code_challenge",
      "code_challenge_method",
      "decision",
    ]);
    expect(decisionSchema.properties.scope).toEqual({
      type: "string",
      enum: ["resumes:read", "resumes:write", "resumes:read resumes:write"],
    });
    expect(decisionSchema.properties.decision.enum).toEqual([
      "approve",
      "deny",
    ]);
    expect(decision.requestBody.description).toMatch(/4,096 bytes/);

    for (const operationId of Object.keys(
      OPERATIONS,
    ) as (keyof typeof OPERATIONS)[]) {
      const op = operation(operationId);
      const success =
        op.responses[operationId === "revokeAgentGrant" ? "204" : "200"];
      if (success.content) {
        const payload = responseDataSchema(success);
        expect(payload.additionalProperties, operationId).toBe(false);
        expect(JSON.stringify(payload), operationId).not.toMatch(
          /access_token|refresh_token|authorization_code|\btoken\b|\bcode\b/i,
        );
      }
    }
    expect(
      responseDataSchema(operation("postOAuthConsentDecision").responses["200"])
        .properties.redirectTo.description,
    ).toMatch(/registered redirect/i);
  });

  it("uses the shared closed error envelope for documented failures", () => {
    const expected: Record<string, string[]> = {
      getOAuthConsent: ["400", "401", "404"],
      postOAuthConsentDecision: ["400", "401", "403", "404", "415"],
      listAgentGrants: ["401"],
      revokeAgentGrant: ["401", "403", "404"],
    };
    for (const [operationId, statuses] of Object.entries(expected)) {
      const op = operation(operationId as keyof typeof OPERATIONS);
      for (const status of statuses) {
        const documented = op.responses[status];
        expect(documented, `${operationId} ${status}`).toBeTruthy();
        expect(
          response(documented).content["application/json"].schema.$ref,
        ).toBe("#/components/schemas/Error");
      }
    }
  });

  it("documents the exact status matrix, including strict media type rejection", () => {
    const expected: Record<string, string[]> = {
      getOAuthConsent: ["200", "400", "401", "404"],
      postOAuthConsentDecision: [
        "200",
        "400",
        "401",
        "403",
        "404",
        "413",
        "415",
      ],
      listAgentGrants: ["200", "401"],
      revokeAgentGrant: ["204", "401", "403", "404"],
    };
    for (const [operationId, statuses] of Object.entries(expected)) {
      const op = operation(operationId as keyof typeof OPERATIONS);
      expect(Object.keys(op.responses).sort(), operationId).toEqual(
        [...statuses].sort(),
      );
    }
  });
});
