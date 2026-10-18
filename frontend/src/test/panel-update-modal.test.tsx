import { fireEvent } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import PanelUpdateModal, { type PanelUpdateInfo } from '@/pages/index/PanelUpdateModal';
import { HttpUtil, Msg } from '@/utils';
import { renderWithProviders } from './test-utils';

function stableReleaseSection(info: PanelUpdateInfo) {
  vi.mocked(HttpUtil.get).mockImplementation(async () => new Msg(true, '', []));
  renderWithProviders(<PanelUpdateModal open info={info} onClose={vi.fn()} />);

  const section = document.querySelectorAll<HTMLElement>('.ant-collapse-item')[0];
  if (info.channel === 'development') {
    fireEvent.click(section.querySelector<HTMLElement>('.ant-collapse-header')!);
  }
  return {
    versions: Array.from(section.querySelectorAll('.version-list-item .ant-tag')).map(
      (node) => node.textContent,
    ),
    updateButton: section.querySelector<HTMLButtonElement>('.actions-row button')!,
  };
}

describe('PanelUpdateModal stable release', () => {
  it('shows the latest stable release even when a development build is installed', () => {
    const section = stableReleaseSection({
      channel: 'development',
      currentVersion: '0.0.6_9356f1e',
      latestVersion: 'v0.0.7_abcd123',
      stableLatestVersion: 'v0.0.6',
      updateAvailable: true,
    });
    expect(section.versions).toEqual(['v0.0.6_9356f1e', 'v0.0.6']);
    expect(section.updateButton.disabled).toBe(true);
  });

  it('uses the stable release for a stable installation', () => {
    const section = stableReleaseSection({
      channel: 'stable',
      currentVersion: '0.0.5',
      latestVersion: 'v0.0.6',
      stableLatestVersion: 'v0.0.6',
      updateAvailable: true,
    });
    expect(section.versions).toEqual(['v0.0.5', 'v0.0.6']);
    expect(section.updateButton.disabled).toBe(false);
  });

  it('does not mistake a development build for a missing stable release', () => {
    const section = stableReleaseSection({
      channel: 'development',
      currentVersion: '0.0.6_9356f1e',
      latestVersion: 'v0.0.7_abcd123',
      updateAvailable: true,
    });
    expect(section.versions[1]).toBe('-');
  });

  it('supports the previous stable-channel response format', () => {
    const section = stableReleaseSection({
      channel: 'stable',
      currentVersion: '0.0.5',
      latestVersion: 'v0.0.6',
      updateAvailable: true,
    });
    expect(section.versions[1]).toBe('v0.0.6');
  });
});
