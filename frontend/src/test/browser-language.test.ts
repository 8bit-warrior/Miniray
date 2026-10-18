import { afterEach, describe, expect, it, vi } from 'vitest';

import { CookieManager, IntlUtil, LanguageManager } from '@/utils';

describe('browser-driven panel language', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it.each([
    ['zh', 'zh-CN'],
    ['zh-CN', 'zh-CN'],
    ['zh-TW', 'zh-CN'],
    ['zh-HK', 'zh-CN'],
    ['zh-MO', 'zh-CN'],
    ['zh-SG', 'zh-CN'],
    ['zh-Hant-TW', 'zh-CN'],
    ['ZH-hans', 'zh-CN'],
    ['zh_HK', 'zh-CN'],
    ['en-US', 'en-US'],
    ['ja-JP', 'en-US'],
    ['de-DE', 'en-US'],
    ['fr-FR', 'en-US'],
    ['zho', 'en-US'],
  ])('resolves browser language %s as %s', (language, expected) => {
    vi.stubGlobal('navigator', { language });
    const cookie = vi.spyOn(CookieManager, 'getCookie').mockReturnValue('fa-IR');

    expect(LanguageManager.getLanguage()).toBe(expected);
    expect(cookie).not.toHaveBeenCalled();
  });

  it('loads Simplified Chinese for a Traditional Chinese browser', async () => {
    vi.resetModules();
    vi.stubGlobal('navigator', { language: 'zh-Hant-TW' });
    const utils = await import('@/utils');
    const cookie = vi.spyOn(utils.CookieManager, 'getCookie').mockReturnValue('en-US');
    const { readyI18n } = await import('@/i18n/react');

    const i18n = await readyI18n();

    expect(i18n.language).toBe('zh-CN');
    expect(i18n.t('login')).toBe('登录');
    expect(cookie).not.toHaveBeenCalled();
  });

  it('does not resolve language for invalid dates', () => {
    const getLanguage = vi.spyOn(LanguageManager, 'getLanguage');

    expect(LanguageManager.getLanguage()).toMatch(/^(zh-CN|en-US)$/);
    getLanguage.mockClear();

    expect(IntlUtil.formatDate(null)).toBe('');
    expect(IntlUtil.formatDate(undefined)).toBe('');
    expect(IntlUtil.formatDate('not-a-date')).toBe('');
    expect(getLanguage).not.toHaveBeenCalled();
  });
});
