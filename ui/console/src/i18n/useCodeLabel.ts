import { useI18n } from "vue-i18n";

export function useCodeLabel() {
  const { t, te } = useI18n();
  return (value: string) => {
    const key = `codes.${value}`;
    return te(key) ? t(key) : value;
  };
}
