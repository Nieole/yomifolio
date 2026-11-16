import { useEffect, useState } from 'react';
import { readPreference, writePreference } from '../../utils/browserPreferences';

export function useStickyState<T>(
  defaultValue: T,
  key: string,
): [T, React.Dispatch<React.SetStateAction<T>>] {
  const [value, setValue] = useState<T>(() => {
    try {
      const stickyValue = readPreference(key);
      return stickyValue !== null ? JSON.parse(stickyValue) : defaultValue;
    } catch {
      return defaultValue;
    }
  });

  useEffect(() => {
    writePreference(key, JSON.stringify(value));
  }, [key, value]);

  return [value, setValue];
}
