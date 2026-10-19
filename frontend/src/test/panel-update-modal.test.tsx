import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import PanelUpdateModal, { type PanelUpdateInfo } from '@/pages/index/PanelUpdateModal';
import { HttpUtil, Msg } from '@/utils';
import { renderWithProviders } from './test-utils';

function stableReleaseSection(info: PanelUpdateInfo) {
  renderWithProviders(<PanelUpdateModal open info={info} onClose={vi.fn()} />);

  const header = screen.getByText('Stable release');
  const section = header.closest<HTMLElement>('.ant-collapse-item')!;
  if (info.channel === 'development') {
    fireEvent.click(header);
  }
  return {
    versions: Array.from(section.querySelectorAll('.version-list-item .ant-tag')).map(
      (node) => node.textContent,
    ),
    updateButton: within(section).getByRole<HTMLButtonElement>('button', { name: /Update Panel/ }),
  };
}

async function confirmUpdate(
  version: string,
  params?: { channel: 'stable' } | { actionRunId: string },
) {
  const dialog = screen.getByRole('dialog', { name: 'Do you really want to update the panel?' });
  expect(dialog.textContent).toContain(
    `This will update miniray to ${version} and restart the panel service.`,
  );
  fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }));
  await waitFor(() => {
    expect(HttpUtil.post).toHaveBeenCalledWith('/panel/api/server/updatePanel', params);
  });
}

describe('PanelUpdateModal', () => {
  beforeEach(() => {
    vi.mocked(HttpUtil.get).mockReset().mockResolvedValue(new Msg(true, '', []));
    vi.mocked(HttpUtil.post).mockReset().mockResolvedValue(new Msg(false));
  });

  it.each([
    { currentVersion: '0.0.6_9356f1e', latestVersion: 'v0.0.7_abcd123', updateAvailable: true },
    { currentVersion: '0.0.7_9356f1e', latestVersion: 'v0.0.7_9356f1e', updateAvailable: false },
  ])('updates development to stable when updateAvailable=$updateAvailable', async (info) => {
    const section = stableReleaseSection({
      channel: 'development',
      ...info,
      stableLatestVersion: 'v0.0.6',
    });
    expect(section.versions[1]).toBe('v0.0.6');
    expect(section.updateButton.disabled).toBe(false);
    fireEvent.click(section.updateButton);
    await confirmUpdate('v0.0.6', { channel: 'stable' });
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

  it('keeps the stable update disabled when the latest stable release is already installed', () => {
    const section = stableReleaseSection({
      channel: 'stable',
      currentVersion: '0.0.6',
      latestVersion: 'v0.0.6',
      stableLatestVersion: 'v0.0.6',
      updateAvailable: false,
    });
    expect(section.updateButton.disabled).toBe(true);
  });

  it('does not mistake a development build for a missing stable release', () => {
    const section = stableReleaseSection({
      channel: 'development',
      currentVersion: '0.0.6_9356f1e',
      latestVersion: 'v0.0.7_abcd123',
      updateAvailable: true,
    });
    expect(section.versions[1]).toBe('-');
    expect(section.updateButton.disabled).toBe(true);
  });

  it('supports the previous stable-channel response format', async () => {
    const section = stableReleaseSection({
      channel: 'stable',
      currentVersion: '0.0.5',
      latestVersion: 'v0.0.6',
      updateAvailable: true,
    });
    expect(section.versions[1]).toBe('v0.0.6');
    fireEvent.click(section.updateButton);
    await confirmUpdate('v0.0.6', { channel: 'stable' });
  });

  it('keeps the development update shortcut on the development channel', async () => {
    renderWithProviders(
      <PanelUpdateModal
        open
        info={{
          channel: 'development',
          currentVersion: '0.0.6_9356f1e',
          latestVersion: 'v0.0.7_abcd123',
          stableLatestVersion: 'v0.0.6',
          updateAvailable: true,
        }}
        onClose={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /Update Panel/ }));
    await confirmUpdate('v0.0.7_abcd123');
  });

  it('installs the selected development build instead of either channel latest', async () => {
    vi.mocked(HttpUtil.get).mockResolvedValue(
      new Msg(true, '', [
        {
          runId: '315',
          commit: 'abc1234',
          version: 'v0.0.5_abc1234',
          createdAt: '2026-10-18T00:00:00Z',
          title: 'Previous development build',
        },
      ]),
    );
    renderWithProviders(
      <PanelUpdateModal
        open
        info={{
          channel: 'stable',
          currentVersion: '0.0.6',
          latestVersion: 'v0.0.6',
          stableLatestVersion: 'v0.0.6',
          updateAvailable: false,
        }}
        onClose={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByText('Development test builds'));
    fireEvent.click(await screen.findByRole('radio', { name: 'Install build v0.0.5_abc1234' }));
    await confirmUpdate('v0.0.5_abc1234', { actionRunId: '315' });
  });
});
