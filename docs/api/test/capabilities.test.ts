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

  it("returns two required booleans and the enabled providers in the data envelope", () => {
    const schema = doc.components.schemas.Capabilities;
    expect(schema.type).toBe("object");
    expect(schema.additionalProperties).toBe(false);
    expect(schema.required.sort()).toEqual([
      "agentAccess",
      "passkeyEnrollment",
      "passwordRegistration",
      "providerLogin",
      "providers",
    ]);
    expect(schema.properties.passwordRegistration.type).toBe("boolean");
    expect(schema.properties.providerLogin.type).toBe("boolean");
    expect(schema.properties.agentAccess.type).toBe("boolean");
    expect(schema.properties.passkeyEnrollment.type).toBe("boolean");
    expect(schema.properties.providers.type).toBe("array");
    expect(schema.properties.providers.uniqueItems).toBe(true);
    expect(schema.properties.providers.items.enum).toEqual([
      "google",
      "github",
      "linkedin",
    ]);
    const ok = op.responses["200"].content["application/json"].schema;
    const data = ok.allOf.find((part: any) => part.properties?.data);
    expect(data.properties.data.$ref).toBe("#/components/schemas/Capabilities");
  });

  it("documents no-store caching", () => {
    expect(op.description).toMatch(/no-store/);
  });
});

describe("provider operations are conditional", () => {
  for (const provider of ["google", "github", "linkedin"]) {
    for (const path of [
      `/auth/${provider}/start`,
      `/auth/${provider}/callback`,
    ]) {
      it(`${path} says it is registered only when PROVIDER_LOGIN_ENABLED enables ${provider}`, () => {
        for (const method of Object.keys(doc.paths[path])) {
          expect(doc.paths[path][method].description).toContain(
            `\`PROVIDER_LOGIN_ENABLED\` enables \`${provider}\``,
          );
        }
      });
    }
  }
});
