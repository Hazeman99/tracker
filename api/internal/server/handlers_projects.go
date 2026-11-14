package server

import (
	"net/http"

	"tracker/api/internal/auth"
	"tracker/api/internal/store"
)

func userID(r *http.Request) string { return sessionFrom(r).UserID }

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListProjects(r.Context(), userID(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": list})
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.store.GetProject(r.Context(), userID(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type createProjectRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	name, err := singleLine("name", req.Name, maxName)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	desc, err := multiLine("description", req.Description, maxLongText, false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tags, err := tagNames(req.Tags)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.store.CreateProject(r.Context(), userID(r), name, desc, tags)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

type updateProjectRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Tags        *[]string `json:"tags"`
	Archived    *bool     `json:"archived"`
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req updateProjectRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	patch := store.ProjectPatch{Archived: req.Archived}
	if req.Name != nil {
		v, err := singleLine("name", *req.Name, maxName)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		patch.Name = &v
	}
	if req.Description != nil {
		v, err := multiLine("description", *req.Description, maxLongText, false)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		patch.Description = &v
	}
	if req.Tags != nil {
		v, err := tagNames(*req.Tags)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		patch.Tags = &v
	}
	p, err := s.store.UpdateProject(r.Context(), userID(r), id, patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.DeleteProject(r.Context(), userID(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type reorderRequest struct {
	IDs []string `json:"ids"`
}

func (s *Server) reorderProjects(w http.ResponseWriter, r *http.Request) {
	var req reorderRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if len(req.IDs) > maxOrderLength {
		s.fail(w, r, badRequest("too many ids"))
		return
	}
	for _, id := range req.IDs {
		if !auth.ValidID(id) {
			s.fail(w, r, badRequest("invalid id"))
			return
		}
	}
	if err := s.store.ReorderProjects(r.Context(), userID(r), req.IDs); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- tags

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags(r.Context(), userID(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

type renameTagRequest struct {
	Name string `json:"name"`
}

func (s *Server) renameTag(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req renameTagRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	name, err := singleLine("name", req.Name, maxTagName)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	t, err := s.store.RenameTag(r.Context(), userID(r), id, name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// ---------------------------------------------------------------- log

type logRequest struct {
	Body string `json:"body"`
}

func (s *Server) createLogEntry(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req logRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	body, err := multiLine("body", req.Body, maxLongText, true)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	e, err := s.store.CreateLogEntry(r.Context(), userID(r), pid, body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) updateLogEntry(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req logRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	body, err := multiLine("body", req.Body, maxLongText, true)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	e, err := s.store.UpdateLogEntry(r.Context(), userID(r), id, body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) deleteLogEntry(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.DeleteLogEntry(r.Context(), userID(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- tasks

type createTaskRequest struct {
	Title string `json:"title"`
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req createTaskRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	title, err := singleLine("title", req.Title, maxTaskTitle)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	t, err := s.store.CreateTask(r.Context(), userID(r), pid, title)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

type updateTaskRequest struct {
	Title *string `json:"title"`
	Done  *bool   `json:"done"`
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req updateTaskRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	patch := store.TaskPatch{Done: req.Done}
	if req.Title != nil {
		v, err := singleLine("title", *req.Title, maxTaskTitle)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		patch.Title = &v
	}
	t, err := s.store.UpdateTask(r.Context(), userID(r), id, patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.DeleteTask(r.Context(), userID(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
