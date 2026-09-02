import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8")) as any;

describe("GET /capabilities", () => {
  const op = doc.paths["/capabilities"]?.get;

  it("exists, is unauthenticated, and is the only method on its path", () => {
    expect(op?.operationId).toBe("getCapabilities");
    expect(op.security).toEqual([]);
    expect(Object.keys(doc.paths["/capabilities"])).toEqual(["get"]);
  });

  it("returns exactly two required booleans in the data envelope", () => {
    const schema = doc.components.schemas.Capabilities;
    expect(schema.type).toBe("object");
    expect(schema.additionalProperties).toBe(false);
    expect(schema.required.sort()).toEqual(["agentAccess", "providerLogin"]);
    expect(schema.properties.providerLogin.type).toBe("boolean");
    expect(schema.properties.agentAccess.type).toBe("boolean");
    const ok = op.responses["200"].content["application/json"].schema;
    const data = ok.allOf.find((part: any) => part.properties?.data);
    expect(data.properties.data.$ref).toBe(
      "#/components/schemas/Capabilities",
    );
  });

  it("documents no-store caching", () => {
    expect(op.description).toMatch(/no-store/);
  });
});

describe("provider operations are conditional", () => {
  for (const path of [
    "/auth/google/start",
    "/auth/github/start",
    "/auth/linkedin/start",
    "/auth/google/callback",
    "/auth/github/callback",
    "/auth/linkedin/callback",
  ]) {
    it(`${path} says it is registered only when PROVIDER_LOGIN_ENABLED is true`, () => {
      for (const method of Object.keys(doc.paths[path])) {
        expect(doc.paths[path][method].description).toMatch(
          /PROVIDER_LOGIN_ENABLED/,
        );
      }
    });
  }
});
