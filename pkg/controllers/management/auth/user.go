package auth

import (
	"fmt"
	"strings"

	"github.com/rancher/types/apis/management.cattle.io/v3"
	"github.com/rancher/types/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

type userLifecycle struct {
	prtb        v3.ProjectRoleTemplateBindingInterface
	crtb        v3.ClusterRoleTemplateBindingInterface
	grb         v3.GlobalRoleBindingInterface
	users       v3.UserInterface
	prtbLister  v3.ProjectRoleTemplateBindingLister
	crtbLister  v3.ClusterRoleTemplateBindingLister
	grbLister   v3.GlobalRoleBindingLister
	prtbIndexer cache.Indexer
	crtbIndexer cache.Indexer
	grbIndexer  cache.Indexer
}

const crtbByUserRefKey = "auth.management.cattle.io/crtb-by-user-ref"
const prtbByUserRefKey = "auth.management.cattle.io/prtb-by-user-ref"
const grbByUserRefKey = "auth.management.cattle.io/grb-by-user-ref"

func newUserLifecycle(management *config.ManagementContext) *userLifecycle {
	lfc := &userLifecycle{
		prtb:       management.Management.ProjectRoleTemplateBindings(""),
		crtb:       management.Management.ClusterRoleTemplateBindings(""),
		grb:        management.Management.GlobalRoleBindings(""),
		users:      management.Management.Users(""),
		prtbLister: management.Management.ProjectRoleTemplateBindings("").Controller().Lister(),
		crtbLister: management.Management.ClusterRoleTemplateBindings("").Controller().Lister(),
		grbLister:  management.Management.GlobalRoleBindings("").Controller().Lister(),
	}

	prtbInformer := management.Management.ProjectRoleTemplateBindings("").Controller().Informer()
	prtbInformer.AddIndexers(map[string]cache.IndexFunc{
		prtbByUserRefKey: prtbByUserRefFunc,
	})

	lfc.prtbIndexer = prtbInformer.GetIndexer()

	crtbInformer := management.Management.ClusterRoleTemplateBindings("").Controller().Informer()
	crtbInformer.AddIndexers(map[string]cache.IndexFunc{
		crtbByUserRefKey: crtbByUserRefFunc,
	})

	lfc.crtbIndexer = crtbInformer.GetIndexer()

	grbInformer := management.Management.GlobalRoleBindings("").Controller().Informer()
	grbInformer.AddIndexers(map[string]cache.IndexFunc{
		grbByUserRefKey: grbByUserRefFunc,
	})

	lfc.grbIndexer = grbInformer.GetIndexer()

	return lfc
}

func grbByUserRefFunc(obj interface{}) ([]string, error) {
	globalRoleBinding, ok := obj.(*v3.GlobalRoleBinding)
	if !ok {
		return []string{}, nil
	}

	return []string{globalRoleBinding.UserName}, nil
}

func prtbByUserRefFunc(obj interface{}) ([]string, error) {
	projectRoleBinding, ok := obj.(*v3.ProjectRoleTemplateBinding)
	if !ok || projectRoleBinding.UserName == "" {
		return []string{}, nil
	}

	return []string{projectRoleBinding.UserName}, nil
}

func crtbByUserRefFunc(obj interface{}) ([]string, error) {
	clusterRoleBinding, ok := obj.(*v3.ClusterRoleTemplateBinding)
	if !ok || clusterRoleBinding.UserName == "" {
		return []string{}, nil
	}

	return []string{clusterRoleBinding.UserName}, nil
}

func (l *userLifecycle) Create(user *v3.User) (*v3.User, error) {
	var match = false
	for _, id := range user.PrincipalIDs {
		if strings.HasPrefix(id, "local://") {
			match = true
			break
		}
	}

	if !match {
		user.PrincipalIDs = append(user.PrincipalIDs, "local://"+user.Name)
	}

	return user, nil
}

func (l *userLifecycle) Updated(user *v3.User) (*v3.User, error) {
	return user, nil
}

func (l *userLifecycle) Remove(user *v3.User) (*v3.User, error) {
	clusterRoles, err := l.getCRTBByUserName(user.Name)
	if err != nil {
		return nil, err
	}

	err = l.deleteAllCRTB(clusterRoles)
	if err != nil {
		return nil, err
	}

	projectRoles, err := l.getPRTBByUserName(user.Name)
	if err != nil {
		return nil, err
	}

	err = l.deleteAllPRTB(projectRoles)
	if err != nil {
		return nil, err
	}

	globalRoles, err := l.getGRBByUserName(user.Name)
	if err != nil {
		return nil, err
	}

	err = l.deleteAllGRB(globalRoles)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (l *userLifecycle) getCRTBByUserName(username string) ([]*v3.ClusterRoleTemplateBinding, error) {
	obj, err := l.crtbIndexer.ByIndex(crtbByUserRefKey, username)
	if err != nil {
		return nil, fmt.Errorf("error getting cluster roles: %v", err)
	}

	var crtbs []*v3.ClusterRoleTemplateBinding
	for _, o := range obj {
		crtb, ok := o.(*v3.ClusterRoleTemplateBinding)
		if !ok {
			return nil, fmt.Errorf("error converting obj to cluster role template binding: %v", o)
		}

		crtbs = append(crtbs, crtb)
	}

	return crtbs, nil
}

func (l *userLifecycle) getPRTBByUserName(username string) ([]*v3.ProjectRoleTemplateBinding, error) {
	objs, err := l.prtbIndexer.ByIndex(prtbByUserRefKey, username)
	if err != nil {
		return nil, fmt.Errorf("error getting indexed project roles: %v", err)
	}

	var prtbs []*v3.ProjectRoleTemplateBinding
	for _, obj := range objs {
		prtb, ok := obj.(*v3.ProjectRoleTemplateBinding)
		if !ok {
			return nil, fmt.Errorf("could not convert obj to v3.ProjectRoleTemplateBinding")
		}

		prtbs = append(prtbs, prtb)
	}

	return prtbs, nil
}

func (l *userLifecycle) getGRBByUserName(username string) ([]*v3.GlobalRoleBinding, error) {
	objs, err := l.grbIndexer.ByIndex(grbByUserRefKey, username)
	if err != nil {
		return nil, fmt.Errorf("error getting indexed global roles: %v", err)
	}

	var grbs []*v3.GlobalRoleBinding
	for _, obj := range objs {
		grb, ok := obj.(*v3.GlobalRoleBinding)
		if !ok {
			return nil, fmt.Errorf("could not convert obj to v3.GlobalRoleBinding")
		}

		grbs = append(grbs, grb)
	}

	return grbs, nil
}

func (l *userLifecycle) deleteAllCRTB(crtbs []*v3.ClusterRoleTemplateBinding) error {
	for _, crtb := range crtbs {
		var err error
		if crtb.Namespace == "" {
			err = l.crtb.Delete(crtb.Name, &metav1.DeleteOptions{})
		} else {
			err = l.crtb.DeleteNamespaced(crtb.Namespace, crtb.Name, &metav1.DeleteOptions{})
		}
		if err != nil {
			return fmt.Errorf("error deleting cluster role: %v", err)
		}
	}

	return nil
}

func (l *userLifecycle) deleteAllPRTB(prtbs []*v3.ProjectRoleTemplateBinding) error {
	for _, prtb := range prtbs {
		var err error
		if prtb.Namespace == "" {
			err = l.prtb.Delete(prtb.Name, &metav1.DeleteOptions{})
		} else {
			err = l.prtb.DeleteNamespaced(prtb.Namespace, prtb.Name, &metav1.DeleteOptions{})
		}
		if err != nil {
			return fmt.Errorf("error deleting projet role: %v", err)
		}
	}

	return nil
}

func (l *userLifecycle) deleteAllGRB(grbs []*v3.GlobalRoleBinding) error {
	for _, grb := range grbs {
		var err error
		if grb.Namespace == "" {
			err = l.grb.Delete(grb.Name, &metav1.DeleteOptions{})
		} else {
			err = l.grb.DeleteNamespaced(grb.Namespace, grb.Name, &metav1.DeleteOptions{})
		}
		if err != nil {
			return fmt.Errorf("error deleting global role template %v: %v", grb.Name, err)

		}
	}

	return nil
}
