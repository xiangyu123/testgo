package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/util/podutils"
)

var (
	now        = metav1.Now()
	kubeconfig = flag.String("kubeconfig", "/root/.kube/config", "absolute path to the kubeconfig file")
	clientset  *kubernetes.Clientset
)

const (
	Normal              = "Normal"
	Started             = "Started"
	Killing             = "Killing"
	Warning             = "Warning"
	Unhealthy           = "Unhealthy"
	WatchNs             = "prod"
	WatchResource       = "Pod"
)

type PodSvc struct {
	podIP   string
	svcName string
}

func main() {
	klog.InitFlags(nil)
	defer klog.Flush()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		klog.Errorln(err)
	}
	var er error

	clientset, er = kubernetes.NewForConfig(config)
	if er != nil {
		panic(er)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(clientset, time.Second*30)
	eventInformer := kubeInformerFactory.Core().V1().Events().Informer()

	stopper := make(chan struct{})
	defer close(stopper)
	defer runtime.HandleCrash()

	eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    onHandle,
		DeleteFunc: onHandle,
	})

	go eventInformer.Run(stopper)
	if !cache.WaitForCacheSync(stopper, eventInformer.HasSynced) {
		runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}
	<-stopper
}

func CheckEventVaild(event *v1.Event) (result bool) {
	if event.Count == 1 {
		lastTs := event.LastTimestamp

		// 比启动的时间早的事件是无效的
		if now.Before(&lastTs) {
			result = true
		}
	}
	return
}

func podListOptions(podObject *v1.Pod) *metav1.ListOptions {
	allLabels := podObject.GetObjectMeta().GetLabels()
	slectorLabel := map[string]string{"env": allLabels["env"], "logic_group": allLabels["logic_group"], "appcode": allLabels["appcode"]}
	labelSelector := metav1.LabelSelector{MatchLabels: slectorLabel}
	listOptions := &metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
		Limit:         10000,
	}

	return listOptions
}

func getSVCForPod(pod *v1.Pod, ns string, metaOptions *metav1.ListOptions) (*v1.Service, error) {
	SvcList, err := clientset.CoreV1().Services(ns).List(context.TODO(), *metaOptions)
	if err != nil {
		return nil, err
	}

	if len(SvcList.Items) > 0 {
		svcObj := SvcList.Items[0]
		return &svcObj, nil
	}

	return nil, fmt.Errorf("pod: %s, not found service", pod.Name)
}

func UmountUpstreamPod(podName string, event *v1.Event, umchan chan *PodSvc) {
	if ok := CheckEventVaild(event); ok {
		ns := event.InvolvedObject.Namespace
		podObject, err := clientset.CoreV1().Pods(ns).Get(context.TODO(), podName, metav1.GetOptions{})

		if err != nil {
			klog.Errorln(err)
		} else {
			klog.InfoS("pod status changed", "Pod", podName, "Type", event.Type, "Reason", event.Reason, "Action", "Umount", "Uid", event.InvolvedObject.UID)
			lsOptions := podListOptions(podObject)
			go dotask(UmountTaks, podObject, ns, lsOptions)
		}
	}
}

func MountUpstreamPod(podName string, event *v1.Event, mchan chan *PodSvc) {
	if ok := CheckEventVaild(event); ok {
		klog.InfoS("pod status changed", "Pod", podName, "Type", event.Type, "Reason", event.Reason, "Action", "Mount", "Uid", event.InvolvedObject.UID)

		ns := event.InvolvedObject.Namespace
		podReady := wait.PollImmediate(time.Second*5, time.Hour*1, func() (bool, error) {
			if podObject, err := clientset.CoreV1().Pods(ns).Get(context.TODO(), podName, metav1.GetOptions{}); err != nil {
				return false, nil
			} else {
				isReady := podutils.IsPodReady(podObject)

				if isReady {
					return true, nil
				}
			}
			return false, nil
		})

		pod, _ := clientset.CoreV1().Pods(ns).Get(context.TODO(), podName, metav1.GetOptions{})

		if podReady == nil {
			time.Sleep(time.Second * 10)
			lsOptions := podListOptions(pod)
			go dotask(MountTask, pod, ns, lsOptions)
		}
	}
}

func onHandle(obj interface{}) {
	umountChan := make(chan *PodSvc, 4)
	mountChan := make(chan *PodSvc, 4)
	if event, ok := obj.(*v1.Event); ok {
		// 只处理prod命名空间的Pod类型的资源，其他的不处理
		if event.InvolvedObject.Kind == WatchResource && event.InvolvedObject.Namespace == WatchNs {

			// 获取Pod的名字
			pod := event.InvolvedObject.Name
			if event.Type == Normal && event.Reason == Started {
				go MountUpstreamPod(pod, event, mountChan)
			}

			if event.Type == Normal && event.Reason == Killing {
				go UmountUpstreamPod(pod, event, umountChan)
			}
		}
	}
}
