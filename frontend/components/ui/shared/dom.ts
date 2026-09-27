/** Spread onto a root element: `<div {...testIdAttr(testId)} />`. */
export function testIdAttr(testId: string | undefined) {
  return testId ? { "data-testid": testId } : {};
}
