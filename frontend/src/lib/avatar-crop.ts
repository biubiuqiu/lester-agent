export const cropSize = 320;
export type Crop = { zoom: number; rotation: number; x: number; y: number };
export const initialCrop: Crop = { zoom: 1, rotation: 0, x: 0, y: 0 };

// Positions use a fixed logical viewport so export and responsive/touch previews
// stay identical. Clamp after every pan, zoom or rotation to avoid empty edges.
export function cropGeometry(width: number, height: number, crop: Crop) {
  const rotated = Math.abs(crop.rotation % 180) === 90;
  const w = rotated ? height : width, h = rotated ? width : height;
  const zoom = Math.max(1, Math.min(4, crop.zoom));
  const scale = cropSize / Math.min(w, h) * zoom;
  const limitX = (w * scale - cropSize) / 2, limitY = (h * scale - cropSize) / 2;
  return { scale, crop: { ...crop, zoom, x: Math.max(-limitX, Math.min(limitX, crop.x)) || 0, y: Math.max(-limitY, Math.min(limitY, crop.y)) || 0 } };
}

export function drawAvatarCrop(context: CanvasRenderingContext2D, image: ImageBitmap, crop: Crop, size: number) {
  const { scale, crop: bounded } = cropGeometry(image.width, image.height, crop);
  context.clearRect(0, 0, size, size);
  context.save();
  context.scale(size / cropSize, size / cropSize);
  context.translate(cropSize / 2 + bounded.x, cropSize / 2 + bounded.y);
  context.rotate(bounded.rotation * Math.PI / 180);
  context.scale(scale, scale);
  context.imageSmoothingEnabled = true;
  context.imageSmoothingQuality = "high";
  context.drawImage(image, -image.width / 2, -image.height / 2);
  context.restore();
}
