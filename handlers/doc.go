// Package handlers serves REST controllers of models with gin. The responses are JSON documents with the data,
// the meta data of the lists and the errors. ParseQuery and ApplyQuery turn the URL query of a list request into
// the conditions, order and page of a pg.ModelQuery, and the errors of the responses are Error values.
package handlers
