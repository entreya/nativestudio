import { describe, expect, it } from 'vitest';
import { studioThemes, themeVariables } from './themes';

describe('studio themes', () => {
  it('provides ten complete and uniquely named variants', () => {
    expect(studioThemes).toHaveLength(10);
    expect(new Set(studioThemes.map(theme => theme.id)).size).toBe(10);
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
