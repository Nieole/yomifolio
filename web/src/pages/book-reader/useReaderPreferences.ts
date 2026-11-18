import { useStickyState } from './useStickyState';
import { PREFERENCE_KEYS } from '../../utils/browserPreferences';
import type { ImageFilter, ReaderImageFormat, ReadDirection, ReadMode, ScaleMode, ReaderTheme } from './types';

export function useReaderPreferences() {
  const [readerTheme, setReaderTheme] = useStickyState<ReaderTheme>('base', PREFERENCE_KEYS.readerTheme);
  const [readMode, setReadMode] = useStickyState<ReadMode>('webtoon', PREFERENCE_KEYS.readMode);
  const [readDirection, setReadDirection] = useStickyState<ReadDirection>('ltr', PREFERENCE_KEYS.readDirection);
  const [doublePage, setDoublePage] = useStickyState<boolean>(false, PREFERENCE_KEYS.doublePage);
  const [scaleMode, setScaleMode] = useStickyState<ScaleMode>('fit-screen', PREFERENCE_KEYS.scaleMode);
  const [imageFilter, setImageFilter] = useStickyState<ImageFilter>('bilinear', PREFERENCE_KEYS.imageFilter);
  const [autoCrop, setAutoCrop] = useStickyState<boolean>(false, PREFERENCE_KEYS.autoCrop);
  const [preloadCount, setPreloadCount] = useStickyState<number>(3, PREFERENCE_KEYS.preloadCount);
  const [readerImageFormat, setReaderImageFormat] = useStickyState<ReaderImageFormat>('original', PREFERENCE_KEYS.readerImageFormat);
  const [readerImageQuality, setReaderImageQuality] = useStickyState<number>(82, PREFERENCE_KEYS.readerImageQuality);
  const [eyeProtection, setEyeProtection] = useStickyState<boolean>(false, PREFERENCE_KEYS.eyeProtection);
  const [w2xScale, setW2xScale] = useStickyState<number>(2, PREFERENCE_KEYS.w2xScale);
  const [w2xNoise, setW2xNoise] = useStickyState<number>(0, PREFERENCE_KEYS.w2xNoise);
  const [w2xFormat, setW2xFormat] = useStickyState<string>('webp', PREFERENCE_KEYS.w2xFormat);

  return {
    readerTheme,
    setReaderTheme,
    readMode,
    setReadMode,
    readDirection,
    setReadDirection,
    doublePage,
    setDoublePage,
    scaleMode,
    setScaleMode,
    imageFilter,
    setImageFilter,
    autoCrop,
    setAutoCrop,
    preloadCount,
    setPreloadCount,
    readerImageFormat,
    setReaderImageFormat,
    readerImageQuality,
    setReaderImageQuality,
    eyeProtection,
    setEyeProtection,
    w2xScale,
    setW2xScale,
    w2xNoise,
    setW2xNoise,
    w2xFormat,
    setW2xFormat,
  };
}
