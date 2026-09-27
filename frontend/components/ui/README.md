# UI kit

Wrappers around a component library (currently MUI v9). App code depends on the
wrappers' props, never on MUI, so any primitive can move to another library, or
to hand-written code, without touching its callers.

```tsx
import { Button, Input, BottomSheet } from "@/components/ui";
```

## Layout

```
components/ui/
├── index.ts               public entry point: the only import path the app uses
├── provider/UIProvider    MUI cache + theme providers; mounted in app/layout.tsx
├── theme/
│   ├── tokens.ts          colours, radii, fonts (library-agnostic)
│   └── muiTheme.ts        tokens → MUI theme
├── shared/
│   ├── types.ts           shared vocabulary: Size, Tone, BaseProps, SelectOption
│   ├── dom.ts             testIdAttr
│   ├── icons.tsx          plain-SVG icons used inside primitives
│   └── muiAdapters.ts     our vocabulary → MUI's ("sm" → "small", "danger" → "error")
└── primitives/<Name>/
    ├── <Name>.types.ts    the public contract: props and types. No library imports.
    ├── <Name>.tsx         the implementation. The only file that knows about MUI.
    └── index.ts           re-exports the component and its types
```

## Rules

1. **The contract is `<Name>.types.ts`.** Props use our own vocabulary
   (`variant="danger"`, `size="sm"`), never MUI's names or types. Don't add
   `sx`, `slotProps`, or `...rest` spreading onto the MUI component: each of
   those leaks MUI into callers.
2. **Callbacks carry values, not library events.** `onChange(value)`, not
   `onChange(event, value)`. (DOM events such as Button's `onClick` are fine;
   they are part of React, not MUI.)
3. **`className` is the escape hatch for layout.** It targets the root element.
   MUI styles sit in `@layer mui`, which `app/globals.css` orders before
   Tailwind's utilities, so Tailwind classes always win without `!important`.
4. **Composites build on primitives, not on MUI.** `Tabs` uses `TabList`, so
   it never needs to change when the underlying library does.
5. **ESLint enforces the boundary.** `@mui/*`, `@emotion/*` and `react-window`
   can only be imported from `primitives/*/*.tsx`, `provider/` and `theme/`.
   Deep imports like `@/components/ui/primitives/Button` are also blocked.

## Replacing a primitive's implementation

1. Rewrite `primitives/<Name>/<Name>.tsx` against the new library. Leave
   `<Name>.types.ts` and `index.ts` unchanged.
2. Run `npx tsc --noEmit`. If the new implementation can't honour a prop, the
   contract is what has to change. That is a breaking change, so update the
   callers in the same PR.
3. If no primitive uses MUI any more, drop `UIProvider`'s MUI providers,
   `muiTheme.ts` and `muiAdapters.ts`, and remove the packages.

## Where MUI has no equivalent

| Primitive              | Built from                                             |
| ---------------------- | ------------------------------------------------------ |
| Carousel               | CSS scroll-snap track + MUI IconButton/ButtonBase      |
| OTPInput               | one MUI OutlinedInput per character                    |
| VideoRenderer          | `<video>` via MUI Box; supports `src` or a MediaStream |
| VirtualizedList        | react-window (MUI's own recommendation)                |
| HorizontalList         | MUI Stack with horizontal overflow                     |
| BottomSheet            | MUI SwipeableDrawer + URL hash, so Back closes it      |

## Adding a primitive

Create `primitives/<Name>/` with the three files above, then add
`export * from "./primitives/<Name>";` to `index.ts`.
