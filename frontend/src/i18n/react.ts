import i18next from 'i18next';
import { initReactI18next } from 'react-i18next';

import { LanguageManager } from '@/utils';
import enUS from '../../../internal/web/translation/en-US.json';

const FALLBACK = 'en-US';

export async function readyI18n() {
  const active = LanguageManager.getLanguage();

  await i18next.use(initReactI18next).init({
    lng: FALLBACK,
    fallbackLng: FALLBACK,
    resources: { [FALLBACK]: { translation: enUS } },
    interpolation: { escapeValue: false, prefix: '{', suffix: '}' },
    returnNull: false,
  });

  if (active === 'zh-CN') {
    const { default: zhCN } = await import('../../../internal/web/translation/zh-CN.json');
    i18next.addResourceBundle('zh-CN', 'translation', zhCN, true, true);
    await i18next.changeLanguage('zh-CN');
  }
  return i18next;
}

export { i18next as i18n };
