// Retain real OS controls. Only macOS draws content into the title-bar area;
// Windows/Linux keep their normal frame and must not inherit traffic-light offsets.
export function windowChrome(platform) {
  return platform === 'darwin'
    ? { titleBarStyle: 'hiddenInset', trafficLightPosition: { x: 20, y: 18 } }
    : {};
}
