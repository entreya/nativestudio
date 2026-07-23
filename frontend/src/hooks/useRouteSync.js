import { useEffect } from 'react';
import { useParams, useSearchParams, useNavigate } from 'react-router-dom';
import { useAppState } from '../state/useAppState';
import { splatToFilePath } from '../state/fileURL';

// Deep link / hard refresh support: when the URL names a project that isn't
// the one currently loaded, resolve it against the fetched project list and
// select it. Runs again once `projects` finishes loading if it wasn't ready
// yet on the first pass. An unknown project id sends the user back to "/".
export function useProjectRouteSync() {
  const { projectId } = useParams();
  const navigate = useNavigate();
  const { activeProject, projects, projectsLoaded, selectProject } = useAppState();

  useEffect(() => {
    if (!projectId || activeProject?.id === projectId) return;
    const proj = projects.find(p => p.id === projectId);
    if (proj) {
      selectProject(proj);
    } else if (projectsLoaded) {
      navigate('/', { replace: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, projects, projectsLoaded, activeProject]);

  return projectId;
}

// Opens whatever file (and cursor position) the "/files/*" URL segment names,
// once the project it belongs to has finished resolving. Only used by the
// editor route — the URL is the source of truth here, so this must not
// trigger another navigation (that's what openFileFromRoute guarantees).
export function useFileRouteSync(projectId) {
  const params = useParams();
  const [searchParams] = useSearchParams();
  const { activeProject, activeTab, openFileFromRoute } = useAppState();
  const urlFilePath = splatToFilePath(params['*']);

  useEffect(() => {
    if (!activeProject || activeProject.id !== projectId) return;
    if (urlFilePath && urlFilePath !== activeTab) {
      const line = parseInt(searchParams.get('line'), 10) || undefined;
      const column = parseInt(searchParams.get('col'), 10) || undefined;
      openFileFromRoute(urlFilePath, { line, column });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlFilePath, activeProject, projectId]);

  return urlFilePath;
}
