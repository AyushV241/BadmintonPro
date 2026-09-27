import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // UI kit boundary: the rest of the app talks to "@/components/ui" only, so a
  // primitive's underlying library can change without touching callers.
  {
    files: ["**/*.{ts,tsx}"],
    ignores: [
      // Implementation files and the provider/theme are allowed to use MUI.
      "components/ui/primitives/*/*.tsx",
      "components/ui/provider/**",
      "components/ui/theme/**",
    ],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@mui/*", "@emotion/*", "react-window"],
              message:
                "Use the wrappers from \"@/components/ui\". Only a primitive's implementation file may import the underlying library.",
            },
            {
              group: ["@/components/ui/*"],
              message: "Import from \"@/components/ui\", not from inside it.",
            },
          ],
        },
      ],
    },
  },
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
  ]),
]);

export default eslintConfig;
