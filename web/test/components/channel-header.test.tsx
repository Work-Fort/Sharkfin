import { describe, it, expect } from 'vitest';
import { render } from 'solid-js/web';
import { ChannelHeader } from '../../src/components/channel-header';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('ChannelHeader', () => {
  it('renders channel name with hash', () => {
    const el = renderInto(() => <ChannelHeader name="general" />);
    expect(el.querySelector('.sf-main__channel-hash')?.textContent).toBe('#');
    expect(el.querySelector('.sf-main__channel-name')?.textContent).toBe('general');
  });

  it('renders topic when provided', () => {
    const el = renderInto(() => <ChannelHeader name="general" topic="Team updates" />);
    expect(el.querySelector('.sf-main__topic')?.textContent).toBe('Team updates');
  });

  it('renders without topic', () => {
    const el = renderInto(() => <ChannelHeader name="random" />);
    expect(el.querySelector('.sf-main__topic')).toBeFalsy();
  });
});
