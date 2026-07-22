import { describe, expect, it } from 'vitest';
import { studioThemes, themeVariables } from './themes';

describe('studio themes', () => {
  it('provides the default plus twenty complete and uniquely named variants', () => {
    expect(studioThemes).toHaveLength(21);
    expect(studioThemes[0].id).toBe('default');
    expect(new Set(studioThemes.map(theme => theme.id)).size).toBe(21);
    studioThemes.forEach(theme => {
      expect(theme).toEqual(expect.objectContaining({
        id: expect.any(String),
        name: expect.any(String),
        layout: expect.stringMatching(/^(standard|rounded|floating|glass|compact|terminal)$/),
        bg: expect.stringMatching(/^#/),
        surface: expect.stringMatching(/^#/),
        text: expect.stringMatching(/^#/),
        accent: expect.stringMatching(/^#/),
      }));
      expect(themeVariables(theme)['--studio-accent']).toBe(theme.accent);
    });
  });
});
